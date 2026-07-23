package api

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wlix13/orrery/collector/internal/store"
)

var ranges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
	"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour,
}

// Short windows used for "hubs seen recently" (independent of traffic range).
var seenWindows = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
}

// parseRange maps ?range= to a [from, to) unix-second window (default 1h).
func parseRange(r *http.Request) (from, to int64, ok bool) {
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "1h"
	}

	d, found := ranges[name]
	if !found {
		return 0, 0, false
	}

	now := time.Now().Unix()

	return now - int64(d.Seconds()), now, true
}

// parseSeen maps ?seen= to the start of the hubs-seen lookback (default 6h).
func parseSeen(r *http.Request, to int64) (seenFrom int64, ok bool) {
	name := r.URL.Query().Get("seen")
	if name == "" {
		name = "6h"
	}

	d, found := seenWindows[name]
	if !found {
		return 0, false
	}

	return to - int64(d.Seconds()), true
}

// nodeStatus derives the health label used across the API and /metrics.
func (s *Server) nodeStatus(n store.NodeStatus) string {
	if n.Collect == "off" {
		return "off"
	}

	if n.LastOK == 0 {
		return "down"
	}

	age := time.Since(time.Unix(n.LastOK, 0))
	interval := s.cfg.Poll.Interval.D()

	switch {
	case age < 2*interval:
		return "up"
	case age < 5*interval:
		return "stale"
	default:
		return "down"
	}
}

type nodeJSON struct {
	Node         string `json:"node"`
	Fleet        string `json:"fleet"`
	ID           string `json:"id"`
	Region       string `json:"region"`
	Type         string `json:"type"`
	Hostname     string `json:"hostname"`
	Status       string `json:"status"`
	Collect      string `json:"collect"`
	LastErr      string `json:"last_err,omitempty"`
	LastOK       int64  `json:"last_ok"`
	LastErrTS    int64  `json:"last_err_ts,omitempty"`
	UptimeS      int64  `json:"uptime_s"`
	NumGoroutine int64  `json:"num_goroutine"`
	AllocBytes   int64  `json:"alloc_bytes"`
	SysBytes     int64  `json:"sys_bytes"`
	NumGC        int64  `json:"num_gc"`
}

func (s *Server) toNodeJSON(n store.NodeStatus) nodeJSON {
	return nodeJSON{
		Node: n.Key, Fleet: n.Fleet, ID: n.ID, Region: n.Region, Type: n.Type,
		Hostname: n.Hostname, Status: s.nodeStatus(n), Collect: n.Collect,
		LastOK: n.LastOK, LastErr: n.LastErr, LastErrTS: n.LastErrTS,
		UptimeS: n.UptimeS, NumGoroutine: n.NumGoroutine,
		AllocBytes: n.AllocBytes, SysBytes: n.SysBytes, NumGC: n.NumGC,
	}
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseRange(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_range", "range must be one of 1h|6h|24h|7d|30d|90d")
		return
	}

	traffic, err := s.store.OverviewTraffic(r.Context(), from, to, requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	statuses, err := s.store.NodeStatuses(r.Context(), requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	counts := map[string]int{}

	type fleetAgg struct {
		up, total int
	}

	fleetNodes := map[string]*fleetAgg{}

	for _, n := range statuses {
		st := s.nodeStatus(n)
		counts[st]++

		fa, ok := fleetNodes[n.Fleet]
		if !ok {
			fa = &fleetAgg{}
			fleetNodes[n.Fleet] = fa
		}

		fa.total++
		if st == "up" {
			fa.up++
		}
	}

	type fleetJSON struct {
		Fleet      string `json:"fleet"`
		NodesUp    int    `json:"nodes_up"`
		NodesTotal int    `json:"nodes_total"`
		UpBytes    int64  `json:"up_bytes"`
		DownBytes  int64  `json:"down_bytes"`
	}

	fleets := make([]fleetJSON, 0, len(fleetNodes))

	for name, fa := range fleetNodes {
		fb := traffic.FleetBytes[name]
		fleets = append(fleets, fleetJSON{
			Fleet: name, NodesUp: fa.up, NodesTotal: fa.total,
			UpBytes: fb.Up, DownBytes: fb.Down,
		})
	}

	// Map iteration is randomised; clients rely on a stable fleet order.
	slices.SortFunc(fleets, func(a, b fleetJSON) int { return strings.Compare(a.Fleet, b.Fleet) })

	topUsers := make([]map[string]any, 0, len(traffic.TopUsers))
	for _, u := range traffic.TopUsers {
		topUsers = append(topUsers, map[string]any{"email": u.Entity, "up_bytes": u.Up, "down_bytes": u.Down})
	}

	topNodes := make([]map[string]any, 0, len(traffic.TopNodes))
	for _, n := range traffic.TopNodes {
		topNodes = append(topNodes, map[string]any{"node": n.Entity, "up_bytes": n.Up, "down_bytes": n.Down})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().Unix(),
		"nodes": map[string]int{
			"total": len(statuses), "up": counts["up"],
			"stale": counts["stale"], "down": counts["down"],
			// Intentionally-disabled nodes (collect: off) are a calm state,
			// kept out of the "down" alarm count.
			"off": counts["off"],
		},
		"online_users": traffic.OnlineUsers,
		"totals":       map[string]int64{"up_bytes": traffic.Totals.Up, "down_bytes": traffic.Totals.Down},
		"fleets":       fleets,
		"top_users":    topUsers,
		"top_nodes":    topNodes,
	})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	statuses, err := s.store.NodeStatuses(r.Context(), requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	out := make([]nodeJSON, 0, len(statuses))
	for _, n := range statuses {
		out = append(out, s.toNodeJSON(n))
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("fleet") + "/" + r.PathValue("id")

	from, to, ok := parseRange(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_range", "range must be one of 1h|6h|24h|7d|30d|90d")
		return
	}

	statuses, err := s.store.NodeStatuses(r.Context(), requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	var found *store.NodeStatus

	for i := range statuses {
		if statuses[i].Key == key {
			found = &statuses[i]
			break
		}
	}

	if found == nil {
		writeErr(w, http.StatusNotFound, "not_found", "unknown node "+key)
		return
	}

	totals, err := s.store.NodeTotals(r.Context(), key, from, to)
	if err != nil {
		s.internalErr(w, err)
		return
	}

	// Flatten: node row fields at top level + detail slices (DESIGN contract).
	users := make([]map[string]any, 0, len(totals["user"]))
	for _, u := range orEmpty(totals["user"]) {
		users = append(users, map[string]any{
			"email": u.Entity, "up_bytes": u.Up, "down_bytes": u.Down,
		})
	}

	writeJSON(w, http.StatusOK, struct {
		nodeJSON
		Inbounds  []store.EntityTotal `json:"inbounds"`
		Outbounds []store.EntityTotal `json:"outbounds"`
		Users     []map[string]any    `json:"users"`
	}{
		nodeJSON:  s.toNodeJSON(*found),
		Inbounds:  orEmpty(totals["inbound"]),
		Outbounds: orEmpty(totals["outbound"]),
		Users:     users,
	})
}

func orEmpty(list []store.EntityTotal) []store.EntityTotal {
	if list == nil {
		return []store.EntityTotal{}
	}

	return list
}

var seriesSteps = []int64{60, 300, 900, 3600, 21600, 86400}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now().Unix()

	p := store.SeriesParams{
		From:   qInt(q.Get("from"), now-24*3600),
		To:     qInt(q.Get("to"), now),
		Step:   qInt(q.Get("step"), 0),
		Kind:   q.Get("kind"),
		Node:   q.Get("node"),
		Scope:  requestScope(r),
		Type:   q.Get("type"),
		Entity: q.Get("entity"),
		Dir:    q.Get("dir"),
		Agg:    q.Get("agg"),
	}
	switch p.Kind {
	case "inbound", "outbound", "user", "online":
	default:
		writeErr(w, http.StatusBadRequest, "bad_kind", "kind must be inbound|outbound|user|online")
		return
	}

	if p.From >= p.To {
		writeErr(w, http.StatusBadRequest, "bad_range", "from must be before to")
		return
	}

	if p.Step == 0 {
		span := p.To - p.From
		p.Step = seriesSteps[len(seriesSteps)-1]

		for _, st := range seriesSteps {
			if span/st <= 400 {
				p.Step = st
				break
			}
		}
	}

	if p.Step < 60 {
		writeErr(w, http.StatusBadRequest, "bad_step", "step must be >= 60")
		return
	}

	series, err := s.store.Series(r.Context(), p)
	if err != nil {
		if errors.Is(err, store.ErrBadQuery) {
			writeErr(w, http.StatusBadRequest, "bad_query", err.Error())
			return
		}

		s.internalErr(w, err)

		return
	}

	if series == nil {
		series = []store.Series{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from": p.From - p.From%p.Step, "to": p.To, "step": p.Step, "series": series,
	})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseRange(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_range", "range must be one of 1h|6h|24h|7d|30d|90d")
		return
	}

	seenFrom, ok := parseSeen(r, to)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_seen", "seen must be one of 1h|6h|24h")
		return
	}

	users, err := s.store.Users(r.Context(), from, to, seenFrom, requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	if users == nil {
		users = []store.UserRow{}
	}

	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleUserDetail(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")

	from, to, ok := parseRange(r)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_range", "range must be one of 1h|6h|24h|7d|30d|90d")
		return
	}

	seenFrom, ok := parseSeen(r, to)
	if !ok {
		writeErr(w, http.StatusBadRequest, "bad_seen", "seen must be one of 1h|6h|24h")
		return
	}

	total, nodes, seenHubs, ips, err := s.store.UserDetail(r.Context(), email, from, to, seenFrom, requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	visibleIPs := make([]store.OnlineIPRow, 0, len(ips))

	for _, ip := range ips {
		if ip.IP != "" {
			visibleIPs = append(visibleIPs, ip)
		}
	}

	// Store returns EntityTotal (entity=node_key); API contract uses "node".
	nodeRows := make([]map[string]any, 0, len(nodes))
	for _, n := range orEmpty(nodes) {
		nodeRows = append(nodeRows, map[string]any{
			"node": n.Entity, "up_bytes": n.Up, "down_bytes": n.Down,
		})
	}

	if seenHubs == nil {
		seenHubs = []store.UserHubSeen{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"email":      email,
		"up_bytes":   total.Up,
		"down_bytes": total.Down,
		"online_now": len(ips) > 0,
		"nodes":      nodeRows,
		"seen_hubs":  seenHubs,
		"ips":        visibleIPs,
	})
}

func (s *Server) handleOnline(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.OnlineNow(r.Context(), requestScope(r))
	if err != nil {
		s.internalErr(w, err)
		return
	}

	if rows == nil {
		rows = []store.OnlineRow{}
	}

	writeJSON(w, http.StatusOK, rows)
}

// requestScope narrows the caller's scope by an optional ?fleet= filter. A
// fleet outside the caller's scope yields the empty scope, which reads as no
// data rather than confirming the fleet exists.
func requestScope(r *http.Request) store.Scope {
	sc := principalOf(r.Context()).Scope

	fleet := r.URL.Query().Get("fleet")
	if fleet == "" {
		return sc
	}

	if !sc.Permits(fleet) {
		return store.Scope{}
	}

	return store.FleetScope(fleet)
}

func (s *Server) internalErr(w http.ResponseWriter, err error) {
	s.log.Error("api error", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal", "internal error")
}

func qInt(v string, def int64) int64 {
	if v == "" {
		return def
	}

	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}

	return n
}
