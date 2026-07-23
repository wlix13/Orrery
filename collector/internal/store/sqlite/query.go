package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Queries older than this use the hour table (minute retention default is
// 72h; staying under it with margin keeps results complete).
const minuteHorizon = 48 * time.Hour

// scopeCond restricts rows to a scope's fleets against the given nodes-table
// alias. ok is false when the scope can never match, so the caller returns
// empty rather than querying.
func scopeCond(alias string, sc Scope) (cond string, args []any, ok bool) {
	if sc.All() {
		return "", nil, true
	}

	if sc.Empty() {
		return "", nil, false
	}

	fleets := sc.Fleets()
	ph := strings.TrimSuffix(strings.Repeat("?,", len(fleets)), ",")
	args = make([]any, 0, len(fleets))

	for _, f := range fleets {
		args = append(args, f)
	}

	return alias + ".fleet IN (" + ph + ")", args, true
}

func (s *Store) Series(ctx context.Context, p SeriesParams) ([]Series, error) {
	if p.Step <= 0 {
		return nil, fmt.Errorf("%w: step must be positive", errBadQuery)
	}

	p.From -= p.From % p.Step
	if rem := p.To % p.Step; rem != 0 {
		p.To += p.Step - rem
	}

	slots := (p.To - p.From) / p.Step
	if slots <= 0 {
		return nil, fmt.Errorf("%w: empty range", errBadQuery)
	}

	if slots > maxSlots {
		return nil, fmt.Errorf("%w: range/step yields %d slots (max %d)", errBadQuery, slots, maxSlots)
	}

	if p.Kind == "online" {
		return s.onlineSeries(ctx, p, slots)
	}

	table := "traffic_minute"
	if p.Step >= 3600 {
		table = "traffic_hour"
	}

	groupCols := ""

	switch p.Agg {
	case AggNone, "":
		groupCols = ", t.node_key, t.entity, t.dir"
	case AggEntity:
		groupCols = ", t.entity, t.dir"
	case AggNode:
		groupCols = ", t.node_key, t.dir"
	case AggTotal:
		groupCols = ", t.dir"
	default:
		return nil, fmt.Errorf("%w: invalid agg %q", errBadQuery, p.Agg)
	}

	conds := []string{"t.bucket >= ?", "t.bucket < ?", "t.kind = ?"}
	args := []any{p.From, p.Step, p.From, p.To, p.Kind}

	filters := []struct{ cond, val string }{
		{"t.node_key = ?", p.Node},
		{"n.type = ?", p.Type},
		{"t.entity = ?", p.Entity},
		{"t.dir = ?", p.Dir},
	}
	for _, f := range filters {
		if f.val != "" {
			conds = append(conds, f.cond)
			args = append(args, f.val)
		}
	}

	scoped, scopeArgs, ok := scopeCond("n", p.Scope)
	if !ok {
		return nil, nil
	}

	if scoped != "" {
		conds = append(conds, scoped)
		args = append(args, scopeArgs...)
	}

	q := "SELECT ((t.bucket - ?) / ?) AS slot" + groupCols + ", SUM(t.bytes)" +
		" FROM " + table + " t JOIN nodes n ON n.node_key = t.node_key" +
		" WHERE " + strings.Join(conds, " AND ") + " GROUP BY slot" + groupCols

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	series := map[string]*Series{}

	var order []string

	for rows.Next() {
		var slot, bytes int64

		var node, entity, dir string

		var dest []any

		switch p.Agg {
		case AggNone, "":
			dest = []any{&slot, &node, &entity, &dir, &bytes}
		case AggEntity:
			dest = []any{&slot, &entity, &dir, &bytes}
		case AggNode:
			dest = []any{&slot, &node, &dir, &bytes}
		case AggTotal:
			dest = []any{&slot, &dir, &bytes}
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		key := node + "\x00" + entity + "\x00" + dir

		sr, ok := series[key]
		if !ok {
			sr = &Series{Node: node, Entity: entity, Dir: dir, Points: make([]int64, slots)}
			series[key] = sr

			order = append(order, key)
		}

		if slot >= 0 && slot < slots {
			sr.Points[slot] += bytes
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Strings(order)

	out := make([]Series, 0, len(order))
	for _, k := range order {
		out = append(out, *series[k])
	}

	return out, nil
}

func (s *Store) onlineSeries(ctx context.Context, p SeriesParams, slots int64) ([]Series, error) {
	table := "online_minute"
	if p.Step >= 3600 {
		table = "online_hour"
	}

	perNode := p.Agg == AggNone || p.Agg == AggNode || p.Agg == ""
	conds := []string{"o.bucket >= ?", "o.bucket < ?"}
	args := []any{p.From, p.Step, p.From, p.To}

	if p.Node != "" {
		conds = append(conds, "o.node_key = ?")
		args = append(args, p.Node)
	}

	if p.Type != "" {
		conds = append(conds, "n.type = ?")
		args = append(args, p.Type)
	}

	scoped, scopeArgs, ok := scopeCond("n", p.Scope)
	if !ok {
		return nil, nil
	}

	if scoped != "" {
		conds = append(conds, scoped)
		args = append(args, scopeArgs...)
	}
	// Per-slot: MAX per node (gauge), then SUM across nodes when aggregating.
	inner := "SELECT ((o.bucket - ?) / ?) AS slot, o.node_key AS nk, MAX(o.count) AS c" +
		" FROM " + table + " o JOIN nodes n ON n.node_key = o.node_key" +
		" WHERE " + strings.Join(conds, " AND ") + " GROUP BY slot, nk"
	q := inner

	if !perNode {
		q = "SELECT slot, SUM(c) FROM (" + inner + ") GROUP BY slot"
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	series := map[string]*Series{}

	var order []string

	for rows.Next() {
		var slot, count int64

		var node string

		var dest []any
		if perNode {
			dest = []any{&slot, &node, &count}
		} else {
			dest = []any{&slot, &count}
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		sr, ok := series[node]
		if !ok {
			sr = &Series{Node: node, Points: make([]int64, slots)}
			series[node] = sr

			order = append(order, node)
		}

		if slot >= 0 && slot < slots {
			sr.Points[slot] = count
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Strings(order)

	out := make([]Series, 0, len(order))
	for _, k := range order {
		out = append(out, *series[k])
	}

	return out, nil
}

func trafficTable(from, to int64) string {
	if time.Since(time.Unix(from, 0)) > minuteHorizon {
		return "traffic_hour"
	}

	_ = to

	return "traffic_minute"
}

func (s *Store) NodeStatuses(ctx context.Context, scope Scope) ([]NodeStatus, error) {
	scoped, args, ok := scopeCond("nodes", scope)
	if !ok {
		return nil, nil
	}

	where := ""
	if scoped != "" {
		where = " WHERE " + scoped
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT node_key, fleet, id, region, type, hostname, collect,
		       last_ok, last_err, last_err_ts, uptime_s, num_goroutine, alloc_bytes, sys_bytes, num_gc
		FROM nodes`+where+` ORDER BY fleet, id`, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var out []NodeStatus

	for rows.Next() {
		var n NodeStatus
		if err := rows.Scan(&n.Key, &n.Fleet, &n.ID, &n.Region, &n.Type, &n.Hostname, &n.Collect,
			&n.LastOK, &n.LastErr, &n.LastErrTS, &n.UptimeS, &n.NumGoroutine, &n.AllocBytes, &n.SysBytes, &n.NumGC); err != nil {
			return nil, err
		}

		out = append(out, n)
	}

	return out, rows.Err()
}

// NodeTotals returns per-entity traffic totals for one node over a range,
// keyed by kind.
func (s *Store) NodeTotals(ctx context.Context, nodeKey string, from, to int64) (map[string][]EntityTotal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, entity, dir, SUM(bytes) FROM `+trafficTable(from, to)+`
		WHERE node_key = ? AND bucket >= ? AND bucket < ?
		GROUP BY kind, entity, dir`, nodeKey, from, to)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	acc := map[string]map[string]*EntityTotal{}

	for rows.Next() {
		var kind, entity, dir string

		var bytes int64
		if err := rows.Scan(&kind, &entity, &dir, &bytes); err != nil {
			return nil, err
		}

		if acc[kind] == nil {
			acc[kind] = map[string]*EntityTotal{}
		}

		et, ok := acc[kind][entity]
		if !ok {
			et = &EntityTotal{Entity: entity}
			acc[kind][entity] = et
		}

		if dir == "up" {
			et.Up += bytes
		} else {
			et.Down += bytes
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := map[string][]EntityTotal{}

	for kind, m := range acc {
		list := make([]EntityTotal, 0, len(m))
		for _, et := range m {
			list = append(list, *et)
		}

		sort.Slice(list, func(i, j int) bool {
			return list[i].Up+list[i].Down > list[j].Up+list[j].Down
		})

		out[kind] = list
	}

	return out, nil
}

// OverviewTraffic aggregates inside the scope. Totals and top-N are computed
// with the scope in the query: filtering afterwards would return a truncated
// top-N and totals covering fleets the caller cannot see.
func (s *Store) OverviewTraffic(ctx context.Context, from, to int64, scope Scope) (*OverviewTraffic, error) {
	table := trafficTable(from, to)

	ov := &OverviewTraffic{}

	scoped, scopeArgs, ok := scopeCond("n", scope)
	if !ok {
		return ov, nil
	}

	and := ""
	if scoped != "" {
		and = " AND " + scoped
	}

	var err error
	if ov.Totals, ov.FleetBytes, err = s.fleetTotals(ctx, table, from, to, scope); err != nil {
		return nil, err
	}

	args := append([]any{from, to}, scopeArgs...)

	if ov.TopUsers, err = s.topTotals(ctx, `
		SELECT t.entity, t.dir, SUM(t.bytes)
		FROM `+table+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE t.bucket >= ? AND t.bucket < ? AND t.kind = 'user' AND n.type = 'hub'`+and+`
		GROUP BY t.entity, t.dir`, args...); err != nil {
		return nil, err
	}

	if ov.TopNodes, err = s.topTotals(ctx, `
		SELECT t.node_key, t.dir, SUM(t.bytes)
		FROM `+table+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE t.bucket >= ? AND t.bucket < ? AND t.kind = 'inbound'`+and+`
		GROUP BY t.node_key, t.dir`, args...); err != nil {
		return nil, err
	}

	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT oc.email)
		FROM online_current oc JOIN nodes n ON n.node_key = oc.node_key
		WHERE n.type = 'hub'`+and, scopeArgs...).Scan(&ov.OnlineUsers); err != nil {
		return nil, err
	}

	return ov, nil
}

// fleetTotals sums hub-inbound traffic per fleet and overall.
func (s *Store) fleetTotals(ctx context.Context, table string, from, to int64, scope Scope) (DirTotal, map[string]DirTotal, error) {
	var totals DirTotal

	fleets := map[string]DirTotal{}

	scoped, scopeArgs, ok := scopeCond("n", scope)
	if !ok {
		return totals, fleets, nil
	}

	and := ""
	if scoped != "" {
		and = " AND " + scoped
	}

	args := append([]any{from, to}, scopeArgs...)

	rows, err := s.db.QueryContext(ctx, `
		SELECT n.fleet, t.dir, SUM(t.bytes)
		FROM `+table+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE t.bucket >= ? AND t.bucket < ? AND t.kind = 'inbound' AND n.type = 'hub'`+and+`
		GROUP BY n.fleet, t.dir`, args...)
	if err != nil {
		return totals, nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var fleet, dir string

		var bytes int64
		if err := rows.Scan(&fleet, &dir, &bytes); err != nil {
			return totals, nil, err
		}

		ft := fleets[fleet]
		if dir == "up" {
			ft.Up += bytes
			totals.Up += bytes
		} else {
			ft.Down += bytes
			totals.Down += bytes
		}

		fleets[fleet] = ft
	}

	return totals, fleets, rows.Err()
}

// topTotals runs an (entity, dir, bytes) aggregate query and returns the
// ten biggest entities by combined traffic.
func (s *Store) topTotals(ctx context.Context, query string, args ...any) ([]EntityTotal, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	m := map[string]*EntityTotal{}

	for rows.Next() {
		var entity, dir string

		var bytes int64
		if err := rows.Scan(&entity, &dir, &bytes); err != nil {
			return nil, err
		}

		et, ok := m[entity]
		if !ok {
			et = &EntityTotal{Entity: entity}
			m[entity] = et
		}

		if dir == "up" {
			et.Up += bytes
		} else {
			et.Down += bytes
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	list := make([]EntityTotal, 0, len(m))
	for _, et := range m {
		list = append(list, *et)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Up+list[i].Down > list[j].Up+list[j].Down
	})

	if len(list) > 10 {
		list = list[:10]
	}

	return list, nil
}

func (s *Store) Users(ctx context.Context, from, to, seenFrom int64, scope Scope) ([]UserRow, error) {
	scoped, scopeArgs, ok := scopeCond("n", scope)
	if !ok {
		return nil, nil
	}

	table := trafficTable(from, to)
	where := "t.bucket >= ? AND t.bucket < ? AND t.kind = 'user' AND n.type = 'hub'"
	args := []any{from, to}

	if scoped != "" {
		where += " AND " + scoped

		args = append(args, scopeArgs...)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.entity, t.dir, SUM(t.bytes)
		FROM `+table+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE `+where+` GROUP BY t.entity, t.dir`, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	users := map[string]*UserRow{}

	for rows.Next() {
		var entity, dir string

		var bytes int64
		if err := rows.Scan(&entity, &dir, &bytes); err != nil {
			return nil, err
		}

		u, ok := users[entity]
		if !ok {
			u = &UserRow{Email: entity}
			users[entity] = u
		}

		if dir == "up" {
			u.Up += bytes
		} else {
			u.Down += bytes
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	hubsByEmail, err := s.hubsSeen(ctx, seenFrom, to, scope, "")
	if err != nil {
		return nil, err
	}

	online, err := s.onlineEmails(ctx, scope)
	if err != nil {
		return nil, err
	}

	out := make([]UserRow, 0, len(users))

	for email, u := range users {
		u.Hubs = hubsByEmail[email]
		if u.Hubs == nil {
			u.Hubs = []UserHubSeen{}
		}

		u.OnlineNow = online[email]
		out = append(out, *u)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Up+out[i].Down > out[j].Up+out[j].Down })

	return out, nil
}

// hubsSeen returns hubs with traffic or online presence in [seenFrom, to).
// emailFilter empty = all users; fleet empty = all fleets.
func (s *Store) hubsSeen(ctx context.Context, seenFrom, to int64, scope Scope, emailFilter string) (map[string][]UserHubSeen, error) {
	last := map[string]map[string]int64{} // email → node → last_seen

	set := func(email, node string, ts int64) {
		if last[email] == nil {
			last[email] = map[string]int64{}
		}

		if ts > last[email][node] {
			last[email][node] = ts
		}
	}

	scoped, scopeArgs, ok := scopeCond("n", scope)
	if !ok {
		return map[string][]UserHubSeen{}, nil
	}

	table := trafficTable(seenFrom, to)
	where := "t.bucket >= ? AND t.bucket < ? AND t.kind = 'user' AND n.type = 'hub'"
	args := []any{seenFrom, to}

	if scoped != "" {
		where += " AND " + scoped

		args = append(args, scopeArgs...)
	}

	if emailFilter != "" {
		where += " AND t.entity = ?"

		args = append(args, emailFilter)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.entity, t.node_key, MAX(t.bucket)
		FROM `+table+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE `+where+` GROUP BY t.entity, t.node_key`, args...)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var email, node string

		var ts int64
		if err := rows.Scan(&email, &node, &ts); err != nil {
			rows.Close()
			return nil, err
		}

		set(email, node, ts)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}

	rows.Close()

	onlineWhere := "n.type = 'hub' AND oc.last_seen >= ? AND oc.last_seen < ?"
	onlineArgs := []any{seenFrom, to}

	if scoped != "" {
		onlineWhere += " AND " + scoped

		onlineArgs = append(onlineArgs, scopeArgs...)
	}

	if emailFilter != "" {
		onlineWhere += " AND oc.email = ?"

		onlineArgs = append(onlineArgs, emailFilter)
	}

	orows, err := s.db.QueryContext(ctx, `
		SELECT oc.email, oc.node_key, MAX(oc.last_seen)
		FROM online_current oc JOIN nodes n ON n.node_key = oc.node_key
		WHERE `+onlineWhere+` GROUP BY oc.email, oc.node_key`, onlineArgs...)
	if err != nil {
		return nil, err
	}

	defer orows.Close()

	for orows.Next() {
		var email, node string

		var ts int64
		if err := orows.Scan(&email, &node, &ts); err != nil {
			return nil, err
		}

		set(email, node, ts)
	}

	if err := orows.Err(); err != nil {
		return nil, err
	}

	out := make(map[string][]UserHubSeen, len(last))
	for email, nodes := range last {
		list := make([]UserHubSeen, 0, len(nodes))
		for node, ts := range nodes {
			list = append(list, UserHubSeen{Node: node, LastSeen: ts})
		}

		sort.Slice(list, func(i, j int) bool {
			if list[i].LastSeen != list[j].LastSeen {
				return list[i].LastSeen > list[j].LastSeen
			}

			return list[i].Node < list[j].Node
		})
		out[email] = list
	}

	return out, nil
}

// onlineEmails is scoped: presence on a fleet the caller cannot see must not
// surface as an "online" flag on a user it can.
func (s *Store) onlineEmails(ctx context.Context, scope Scope) (map[string]bool, error) {
	scoped, args, ok := scopeCond("n", scope)
	if !ok {
		return map[string]bool{}, nil
	}

	and := ""
	if scoped != "" {
		and = " AND " + scoped
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT oc.email FROM online_current oc
		JOIN nodes n ON n.node_key = oc.node_key WHERE n.type = 'hub'`+and, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	m := map[string]bool{}

	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}

		m[e] = true
	}

	return m, rows.Err()
}

func (s *Store) UserDetail(ctx context.Context, email string, from, to, seenFrom int64, scope Scope) (DirTotal, []EntityTotal, []UserHubSeen, []OnlineIPRow, error) {
	scoped, scopeArgs, ok := scopeCond("n", scope)
	if !ok {
		return DirTotal{}, nil, nil, nil, nil
	}

	and := ""
	if scoped != "" {
		and = " AND " + scoped
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT t.node_key, t.dir, SUM(t.bytes)
		FROM `+trafficTable(from, to)+` t JOIN nodes n ON n.node_key = t.node_key
		WHERE t.bucket >= ? AND t.bucket < ? AND t.kind = 'user' AND t.entity = ?`+and+`
		GROUP BY t.node_key, t.dir`, append([]any{from, to, email}, scopeArgs...)...)
	if err != nil {
		return DirTotal{}, nil, nil, nil, err
	}

	var total DirTotal

	perNode := map[string]*EntityTotal{}

	for rows.Next() {
		var nodeKey, dir string

		var bytes int64
		if err := rows.Scan(&nodeKey, &dir, &bytes); err != nil {
			rows.Close()
			return DirTotal{}, nil, nil, nil, err
		}

		et, ok := perNode[nodeKey]
		if !ok {
			et = &EntityTotal{Entity: nodeKey}
			perNode[nodeKey] = et
		}

		if dir == "up" {
			et.Up += bytes
			total.Up += bytes
		} else {
			et.Down += bytes
			total.Down += bytes
		}
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return DirTotal{}, nil, nil, nil, err
	}

	nodes := make([]EntityTotal, 0, len(perNode))
	for _, et := range perNode {
		nodes = append(nodes, *et)
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Up+nodes[i].Down > nodes[j].Up+nodes[j].Down })

	hubsByEmail, err := s.hubsSeen(ctx, seenFrom, to, scope, email)
	if err != nil {
		return DirTotal{}, nil, nil, nil, err
	}

	seen := hubsByEmail[email]
	if seen == nil {
		seen = []UserHubSeen{}
	}

	ipRows, err := s.db.QueryContext(ctx, `
		SELECT node_key, ip, last_seen FROM online_current
		WHERE email = ? ORDER BY last_seen DESC`, email)
	if err != nil {
		return DirTotal{}, nil, nil, nil, err
	}

	defer ipRows.Close()

	var ips []OnlineIPRow

	for ipRows.Next() {
		var r OnlineIPRow
		if err := ipRows.Scan(&r.Node, &r.IP, &r.LastSeen); err != nil {
			return DirTotal{}, nil, nil, nil, err
		}

		ips = append(ips, r)
	}

	return total, nodes, seen, ips, ipRows.Err()
}

func (s *Store) OnlineNow(ctx context.Context, scope Scope) ([]OnlineRow, error) {
	scoped, args, ok := scopeCond("n", scope)
	if !ok {
		return nil, nil
	}

	where := ""
	if scoped != "" {
		where = " WHERE " + scoped
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT oc.node_key, oc.email, oc.ip, oc.last_seen FROM online_current oc
		JOIN nodes n ON n.node_key = oc.node_key`+where+`
		ORDER BY oc.node_key, oc.email, oc.last_seen DESC`, args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var out []OnlineRow

	for rows.Next() {
		var nodeKey, email, ip string

		var lastSeen int64
		if err := rows.Scan(&nodeKey, &email, &ip, &lastSeen); err != nil {
			return nil, err
		}

		if len(out) == 0 || out[len(out)-1].Node != nodeKey || out[len(out)-1].Email != email {
			out = append(out, OnlineRow{Node: nodeKey, Email: email})
		}

		if ip != "" {
			last := &out[len(out)-1]
			last.IPs = append(last.IPs, OnlineIPRow{Node: nodeKey, IP: ip, LastSeen: lastSeen})
		}
	}

	return out, rows.Err()
}

func (s *Store) Counters(ctx context.Context, scope Scope) ([]CounterRow, error) {
	scoped, args, ok := scopeCond("n", scope)
	if !ok {
		return nil, nil
	}

	where := ""
	if scoped != "" {
		where = " WHERE " + scoped
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT c.node_key, c.name, c.value FROM counters_last c"+
			" JOIN nodes n ON n.node_key = c.node_key"+where+
			" ORDER BY c.node_key, c.name", args...)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var out []CounterRow

	for rows.Next() {
		var r CounterRow
		if err := rows.Scan(&r.NodeKey, &r.Name, &r.Value); err != nil {
			return nil, err
		}

		out = append(out, r)
	}

	return out, rows.Err()
}
