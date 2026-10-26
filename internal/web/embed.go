package web

import (
	"embed"
	"io/fs"
)

// Frontend hasil `cd web && pnpm run build` (rsbuild → internal/web/dist)
// ikut di-embed ke binary: satu file `go build` sudah berisi UI + API.
// Pola `all:` ikut menyertakan file berawalan titik, jadi `.gitkeep`
// membuat build tetap jalan walau frontend belum dibangun.
//
//go:embed all:dist
var distFS embed.FS

// FrontendFS: isi folder dist (root = index.html, assets/, …).
func FrontendFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// HasFrontend: index.html ada di dalam binary (frontend sudah dibangun).
func HasFrontend() bool {
	sub := FrontendFS()
	if sub == nil {
		return false
	}
	_, err := fs.Stat(sub, "index.html")
	return err == nil
}
