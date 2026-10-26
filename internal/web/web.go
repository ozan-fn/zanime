package web

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"zanime/internal/meta"
	"zanime/internal/torrent"
)

type Server struct {
	t *torrent.Manager
}

func New(tm *torrent.Manager) *Server { return &Server{t: tm} }

// Routes: seluruh route HTTP aplikasi.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("GET /api/streams", s.handleStreams)
	mux.HandleFunc("GET /api/torrent/start", s.t.HandleStart)
	mux.HandleFunc("GET /api/torrent/status/", s.t.HandleStatus)
	mux.HandleFunc("GET /api/torrent/ws/", s.t.HandleWS)
	mux.HandleFunc("GET /api/torrent/stop/", s.t.HandleStop)
	mux.HandleFunc("GET /stream/", s.t.HandleStream)
	mux.HandleFunc("GET /subs/", s.t.HandleSubs)
	mux.Handle("/", fileServer())
	return mux
}

// fileServer: UI dari folder dist yang sudah di-embed ke binary.
func fileServer() http.Handler {
	dist := FrontendFS()
	h := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/stream/") {
			http.NotFound(w, r)
			return
		}
		if !HasFrontend() {
			writeText(w, "Frontend belum dibangun: cd web && pnpm install && pnpm run build", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		if st, err := fs.Stat(dist, name); err != nil || st.IsDir() {
			r.URL.Path = "/" // SPA fallback: path apa pun → index.html
		}
		h.ServeHTTP(w, r)
	})
}

func writeText(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	_, _ = io.WriteString(w, msg+"\n")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// GET /api/search?q=frieren
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, "q kosong", http.StatusBadRequest)
		return
	}
	items, err := meta.SearchAll(q)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, items)
}

// GET /api/meta?type=series&id=tt1
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ, id := q.Get("type"), q.Get("id")
	if typ == "" || id == "" {
		writeErr(w, "type dan id wajib", http.StatusBadRequest)
		return
	}
	m, err := meta.GetMeta(typ, id)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, m)
}

// GET /api/streams?type=series&id=tt…:1:1&q=…
func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	typ, id := q.Get("type"), q.Get("id")
	if typ == "" || id == "" {
		writeErr(w, "type dan id wajib", http.StatusBadRequest)
		return
	}
	streams, err := meta.GetStreams(typ, id)
	if err != nil {
		writeErr(w, err.Error(), http.StatusBadGateway)
		return
	}
	fallback := q.Get("name")
	if fallback == "" {
		fallback = q.Get("q")
	}
	items := make([]meta.Pick, 0, len(streams))
	for _, st := range streams {
		trackers := meta.TrackersFrom(st)
		if trackers == nil {
			trackers = []string{} // jangan kirim null — frontend mengiterasi array ini
		}
		items = append(items, meta.Pick{
			Stream:   st,
			Magnet:   meta.BuildMagnet(st, fallback),
			Trackers: trackers,
		})
	}
	writeJSON(w, items)
}
