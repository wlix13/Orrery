// Package promexp exposes current counters in Prometheus text format so an
// existing Grafana/Prometheus stack can scrape Orrery instead of every node.
// Hand-rolled exposition: the surface is a handful of gauges/counters and
// pulling in client_golang for that buys nothing.
package promexp

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/xray"
)

// Handler renders metrics from the store. status derives the health label
// (the API layer owns that logic; injected to avoid an import cycle).
func Handler(st store.Store, log *slog.Logger, status func(store.NodeStatus) string, scope func(*http.Request) store.Scope) http.Handler {
	// A store failure quotes the database it could not reach.
	fail := func(w http.ResponseWriter, err error) {
		log.Error("metrics error", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := scope(r)

		nodes, err := st.NodeStatuses(r.Context(), sc)
		if err != nil {
			fail(w, err)
			return
		}

		counters, err := st.Counters(r.Context(), sc)
		if err != nil {
			fail(w, err)
			return
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprint(w, "# HELP orrery_node_up 1 when the node's last poll is fresh.\n# TYPE orrery_node_up gauge\n")

		for _, n := range nodes {
			up := 0
			if status(n) == "up" {
				up = 1
			}

			fmt.Fprintf(w, "orrery_node_up{node=%q,fleet=%q,type=%q} %d\n", n.Key, n.Fleet, n.Type, up)
		}

		fmt.Fprint(w, "# HELP orrery_node_last_ok_timestamp_seconds Unix time of the last successful poll.\n# TYPE orrery_node_last_ok_timestamp_seconds gauge\n")

		for _, n := range nodes {
			fmt.Fprintf(w, "orrery_node_last_ok_timestamp_seconds{node=%q} %d\n", n.Key, n.LastOK)
		}

		for _, m := range []struct {
			name, help string
			value      func(store.NodeStatus) int64
		}{
			{"orrery_xray_uptime_seconds", "Xray process uptime.", func(n store.NodeStatus) int64 { return n.UptimeS }},
			{"orrery_xray_goroutines", "Goroutines in the Xray process.", func(n store.NodeStatus) int64 { return n.NumGoroutine }},
			{"orrery_xray_alloc_bytes", "Heap bytes allocated by Xray.", func(n store.NodeStatus) int64 { return n.AllocBytes }},
			{"orrery_xray_sys_bytes", "OS memory obtained by Xray.", func(n store.NodeStatus) int64 { return n.SysBytes }},
		} {
			fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", m.name, m.help, m.name)

			for _, n := range nodes {
				fmt.Fprintf(w, "%s{node=%q} %d\n", m.name, n.Key, m.value(n))
			}
		}

		// Cumulative Xray counters; they reset when Xray restarts, which
		// Prometheus counter semantics (rate/increase) already handle.
		fmt.Fprint(w, "# HELP orrery_traffic_bytes_total Cumulative traffic per Xray stat counter.\n# TYPE orrery_traffic_bytes_total counter\n")

		for _, c := range counters {
			kind, entity, dir, ok := xray.ParseCounterName(c.Name)
			if !ok {
				continue
			}
			// %q escaping (\\, \", \n) matches Prometheus label escaping.
			fmt.Fprintf(w, "orrery_traffic_bytes_total{node=%q,kind=%q,entity=%q,dir=%q} %d\n",
				c.NodeKey, kind, entity, dir, c.Value)
		}
	})
}
