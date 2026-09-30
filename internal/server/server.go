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
		// Referer the stream host wants, so all three levels are proxied. Mode and
		// quality live in the path: the browser resolves the playlist's relative
		// segment URIs against its own URL and drops that URL's query, so segments
		// asked for without them always came back from the default variant.
		api.Get("/hls/{id}/master.m3u8", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			mode := stream.NormMode(req.URL.Query().Get("mode"))
			src, err := stream.CachedResolve(req.Context(), id, mode)
			if err != nil {
				writeErr(w, err)
				return
			}
			stream.StreamMaster(w, req, id, src, mode)
		})
		api.Get("/hls/{id}/{mode}/{quality}/index.m3u8", func(w http.ResponseWriter, req *http.Request) {
			variant, referer, err := variantFor(req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"))
			if err != nil {
				writeErr(w, err)
				return
			}
			stream.StreamVariant(w, req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"), variant, referer)
		})
		// Segmen ditulis sebagai base64url URL absolutnya: upstream menyimpannya
		// di host lain dengan nama berkamuflase (seg-N....jpg), jadi handler tidak
		// bisa merekonstruksi URL dari nama file.
		api.Get("/hls/{id}/{mode}/{quality}/s/{seg}", func(w http.ResponseWriter, req *http.Request) {
			dec, err := base64.RawURLEncoding.DecodeString(chi.URLParam(req, "seg"))
			if err != nil || !strings.HasPrefix(string(dec), "https://") {
				http.NotFound(w, req)
				return
			}

			_, referer, err := variantFor(req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"))
			if err != nil {
				writeErr(w, err)
				return
			}
			// Content type di-sniff dari byte pertama: TS berkamuflase .jpg/.html,
			// MP4 berkamuflase .js. Segmen immutable → cache panjang.
			ct, ok := stream.ProbeSegment(req.Context(), string(dec), referer)
			if !ok {
				writeErr(w, errors.New("segment probe failed"))
				return
			}
			stream.StreamUpstream(w, req, string(dec), referer, ct, "public, max-age=3600")
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
			src, err := stream.CachedResolve(req.Context(), chi.URLParam(req, "id"), req.URL.Query().Get("mode"))
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
			id, mode := chi.URLParam(req, "id"), req.URL.Query().Get("mode")
			lang := subtitle.WantedLang(req)
			job, err := subtitle.Status(req, cache, id, mode, lang)
			if err != nil {
				writeErr(w, err)
				return
			}
			if state, _, _, _ := job.Snapshot(); state != "ready" {
				subtitle.WriteJob(w, writeJSON, http.StatusAccepted, job)
				return
			}
			vtt, ok := cache.Get(subtitle.SubKey(id, mode, lang))
			if !ok {
				writeErr(w, errors.New("finished subtitle is missing from the cache"))
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
			job, err := subtitle.Status(req, cache, chi.URLParam(req, "id"), req.URL.Query().Get("mode"), subtitle.WantedLang(req))
			if err != nil {
				writeErr(w, err)
				return
			}
			subtitle.WriteJob(w, writeJSON, http.StatusOK, job)
		})
	})

	// SPA: semua path non-/api dilayani dari embed — file dist apa adanya
	// (ServeContent mengurus MIME per ekstensi), sisanya index.html supaya
	// preact-router yang memutuskan 404.
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if name != "" && !strings.Contains(name, "..") {
			if data, err := fs.ReadFile(webRoot, name); err == nil {
				http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(data))
				return
			}
		}
		index, err := fs.ReadFile(webRoot, "index.html")
		if err != nil {
			http.Error(w, "web/dist belum dibuild: jalankan `cd web && npm run build` lalu compile ulang", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	return r
}

// variantFor returns the chosen variant playlist URL plus the referer the
// stream host insists on. Mode and quality come from the path, not the query:
// the browser resolves relative segment URIs against the playlist URL and
// drops its query string (RFC 3986), which sent every segment to the default
// variant no matter which quality was picked.
func variantFor(req *http.Request, id, mode, quality string) (variant, referer string, err error) {
	src, err := stream.CachedResolve(req.Context(), id, stream.NormMode(mode))
	if err != nil {
		return "", "", err
	}
	variant, err = stream.PickQuality(src, quality)
	if err != nil {
		return "", "", err
	}
	return variant, src.Referer, nil
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
