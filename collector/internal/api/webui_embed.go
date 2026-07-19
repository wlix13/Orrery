//go:build !nodashboard

// Default build: the SPA ships inside the binary.
// `-tags nodashboard` swaps in webui_nodashboard.go, which drops the embed.

package api

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"strings"
)

// DashboardEmbedded reports whether this binary carries the dashboard.
const DashboardEmbedded = true

// `task dashboard` writes the build here. Only a .gitkeep is committed, so no
// build output lives in git; the `all:` prefix lets that dotfile satisfy the
// embed when the dashboard has never been built.
//
//go:embed all:webui/dist
var webuiFS embed.FS

// dashboardBuilt reports whether a dashboard build was embedded.
func dashboardBuilt() bool {
	_, err := fs.Stat(webuiFS, "webui/dist/index.html")
	return err == nil
}

// spaHandler serves the embedded dashboard with the same semantics as
// Cloudflare's "single-page-application" mode: unknown paths fall back to
// index.html, content-hashed assets cache forever, index.html never caches.
// Icons and the manifest sit at stable paths, so they get a short cache.
func spaHandler() http.Handler {
	sub, err := fs.Sub(webuiFS, "webui/dist")
	if err != nil {
		panic(err) // embed guarantee broken - unreachable
	}

	// Go's table has no .webmanifest entry, so it would fall back to text/plain.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(sub, path); err != nil {
				r.URL.Path = "/" // SPA fallback
			}
		}

		switch {
		case r.URL.Path == "/" || r.URL.Path == "/index.html":
			w.Header().Set("Cache-Control", "no-cache")
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}

		fileServer.ServeHTTP(w, r)
	})
}
