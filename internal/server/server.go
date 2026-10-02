// Package server wires the HTTP API and the embedded SPA.
package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"zanime/internal/hianime"
	"zanime/internal/stream"
	"zanime/internal/subtitle"
)

// reSegment keeps the segment proxy from turning into an open proxy: a plain
// file name in one known directory, never a path or a URL.
var reSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(ts|m4s|mp4)$`)

func New(cache *subtitle.SubtitleCache, webRoot fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Timeout(5*time.Minute), middleware.Recoverer)

	// Semua API hidup di bawah /api/ agar terpisah tegas dari aset SPA.
	r.Route("/api", func(api chi.Router) {
		api.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "limits": subtitle.LlmLimit.Usage()})
		})

		api.Get("/limits", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, subtitle.LlmLimit.Usage())
		})
		api.Get("/search", func(w http.ResponseWriter, req *http.Request) {
			q := strings.TrimSpace(req.URL.Query().Get("q"))
			if q == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q"})
				return
			}
			results, err := hianime.Search(req.Context(), q)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, results)
		})
		// The player loads the master playlist, and the browser cannot send the
		// Referer the stream host wants, so all three levels are proxied. Quality
		// lives in the path: the browser resolves the playlist's relative segment
		// URIs against its own URL and drops that URL's query, so segments asked
		// for without it always came back from the default variant.
		api.Get("/hls/{id}/master.m3u8", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			servePlaylist(w, req, id, func(src *hianime.Source) error {
				return stream.StreamMaster(w, req, id, src)
			})
		})
		api.Get("/hls/{id}/{quality}/index.m3u8", func(w http.ResponseWriter, req *http.Request) {
			id, quality := chi.URLParam(req, "id"), chi.URLParam(req, "quality")
			servePlaylist(w, req, id, func(src *hianime.Source) error {
				variant, err := stream.PickQuality(src, quality)
				if err != nil {
					return err
				}
				return stream.StreamVariant(w, req, id, quality, variant, src.Referer)
			})
		})
		// Segmen ditulis sebagai base64url URL absolutnya: upstream menyimpannya
		// di host lain dengan nama berkamuflase (seg-N....jpg), jadi handler tidak
		// bisa merekonstruksi URL dari nama file.
		api.Get("/hls/{id}/{quality}/s/{seg}", func(w http.ResponseWriter, req *http.Request) {
			dec, err := base64.RawURLEncoding.DecodeString(chi.URLParam(req, "seg"))
			if err != nil || !strings.HasPrefix(string(dec), "https://") {
				http.NotFound(w, req)
				return
			}
			src, err := stream.CachedResolve(req.Context(), chi.URLParam(req, "id"))
			if err != nil {
				writeErr(w, err)
				return
			}
			// Content type di-sniff dari byte pertama: TS berkamuflase .jpg/.html,
			// MP4 berkamuflase .js. Segmen immutable → cache panjang.
			stream.StreamSegment(w, req, string(dec), src.Referer, "public, max-age=3600")
		})
		// The detail page carries what the search card does not: full synopsis,
		// japanese title, airing window, score, studios. Fetched once per browse.
		// /img proxies a remote image so the browser never sees the upstream CDN
		// host. The URL rides in a query param, base64url'd so signs and dots do
		// not invite URL parsing; only http(s) URLs on the known image hosts pass.
		api.Get("/img", func(w http.ResponseWriter, req *http.Request) {
			dec, err := base64.RawURLEncoding.DecodeString(req.URL.Query().Get("u"))
			if err != nil || !hianime.ReImgHost.Match(dec) {
				http.Error(w, "bad image ref", http.StatusBadRequest)
				return
			}
			stream.StreamUpstream(w, req, string(dec), hianime.BaseAPI+"/", "", "public, max-age=86400")
		})
		api.Get("/anime/{id}", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "id")
			detail, err := hianime.AnimeDetails(req.Context(), slug)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		api.Get("/anime/{id}/episodes", func(w http.ResponseWriter, req *http.Request) {
			eps, err := hianime.Episodes(req.Context(), chi.URLParam(req, "id"))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, eps)
		})
		api.Get("/episode/{id}", func(w http.ResponseWriter, req *http.Request) {
			src, err := stream.CachedResolve(req.Context(), chi.URLParam(req, "id"))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, src)
		})
		// /subtitle answers with the converted .vtt once the job finishes, and with
		// 202 + progress until then. A <track> cannot show progress, so the player
		// polls /status and mounts the track only when the file is ready.
		api.Get("/subtitle/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, lang := chi.URLParam(req, "id"), subtitle.WantedLang(req)
			// Cache first: it is the only thing that can be served. A job that is
			// not in there — still converting, failed, or a file the cache refuses —
			// is progress, not an error: 502 was the answer before, for a job that
			// was ready with a file the cache would not take.
			vtt, ok := cache.Get(subtitle.SubKey(id, lang))
			if !ok {
				job, err := subtitle.Status(req, cache, id, lang)
				if err != nil {
					writeErr(w, err)
					return
				}
				subtitle.WriteJob(w, writeJSON, http.StatusAccepted, job)
				return
			}
			w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
			// Tiap panggilan /subtitle/{id} menjalankan subtitleStatus, yang memulai
			// job baru kalau hasilnya belum masuk cache. Tanpa no-store, respons 202
			// yang tersimpan di cache browser menyebabkan poll /status dan fetch
			// .vtt saling membalik: fetch vtt membaca job "converting" lama dari cache
			// HTTP, polling terus menunggu, dan subtitle terpasang hanya setelah
			// refresh menyegarkan cache-nya.
			w.Header().Set("Cache-Control", "no-store")
			io.WriteString(w, vtt)
		})
		api.Get("/subtitle/{id}/status", func(w http.ResponseWriter, req *http.Request) {
			job, err := subtitle.Status(req, cache, chi.URLParam(req, "id"), subtitle.WantedLang(req))
			if err != nil {
				writeErr(w, err)
				return
			}
			subtitle.WriteJob(w, writeJSON, http.StatusOK, job)
		})
	})

	// SPA: semua path non-/api dilayani dari embed — file dist apa adanya
	// (ServeContent mengurus MIME per ekstensi), sisanya index.html supaya
	// router yang memutuskan 404. Aset ber-hash di-cache permanen; index.html
	// dan favicon selalu revalidate, kalau tidak browser bisa menjalankan
	// bundel lama setelah rebuild dan seluruh perilaku SPA jadi usang.
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if name != "" && strings.Contains(name, "..") {
			name = ""
		}
		if data, err := fs.ReadFile(webRoot, name); err == nil {
			if strings.HasPrefix(name, "static/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(data))
			return
		}
		index, err := fs.ReadFile(webRoot, "index.html")
		if err != nil {
			http.Error(w, "web/dist belum dibuild: jalankan `cd web && npm run build` lalu compile ulang", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	return r
}

// servePlaylist resolves the episode, runs fn against the resolved source, and —
// when upstream rejects the cached URLs because their token expired — drops the
// cache entry, re-resolves and retries once. Without the retry the player kept
// hitting the dead URL until the cache TTL happened to lapse, which is exactly
// the recurring "Stream gagal dimuat: kode 1001".
func servePlaylist(w http.ResponseWriter, req *http.Request, id string, fn func(src *hianime.Source) error) {
	src, err := stream.CachedResolve(req.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := fn(src); err == nil {
		return
	}
	stream.Invalidate(id)
	src, err = stream.CachedResolve(req.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := fn(src); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, subtitle.ErrRetryable):
		// The job paces itself, so reaching this means the account limit or a
		// provider upstream is throttling traffic we did not make.
		secs := 30
		var ae subtitle.APIError
		if errors.As(err, &ae) && ae.Retry > 0 {
			secs = int(ae.Retry / time.Second)
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	case errors.Is(err, hianime.ErrCloudflare):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}
