//go:build nodashboard

// Collector-only build: no embedded SPA, and no need for webui/dist to exist
// at compile time.

package api

import "net/http"

const DashboardEmbedded = false

func dashboardBuilt() bool { return false }

// spaHandler is never mounted in this build; Handler gates it on the constant.
func spaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusNotFound, "no_dashboard", "this collector was built without the embedded dashboard")
	})
}
