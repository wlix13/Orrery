package mongo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/wlix13/orrery/collector/internal/store"
)

// Queries older than this use the hour collection (minute retention default
// is 72h; staying under it with margin keeps results complete).
const minuteHorizon = 48 * time.Hour

// trafficColl picks the minute or hour traffic collection for a query
// starting at "from", mirroring sqlite's trafficTable rule.
func (s *Store) trafficColl(from int64) *mongo.Collection {
	if time.Since(time.Unix(from, 0)) > minuteHorizon {
		return s.trafficHour
	}

	return s.trafficMinute
}

// loadNodes reads the whole nodes collection into memory. It backs the
// registered-node scoping that sqlite gets for free via SQL JOINs: since
// Mongo aggregation here avoids $lookup, every query that needs to filter
// by fleet/type or hide unregistered nodes' data does it against this map
// instead. The collection is expected to stay small (one row per node).
func (s *Store) loadNodes(ctx context.Context) (map[string]store.Node, error) {
	cur, err := s.nodes.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	out := map[string]store.Node{}

	for cur.Next(ctx) {
		var doc nodeDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out[doc.Key] = doc.node()
	}

	return out, cur.Err()
}

// filterNodeKeys returns the node keys inside scope matching the given
// type/node filters (empty string = no filter on that dimension).
func filterNodeKeys(nodes map[string]store.Node, scope store.Scope, typ, node string) []string {
	keys := make([]string, 0, len(nodes))

	for k, n := range nodes {
		if !scope.Permits(n.Fleet) {
			continue
		}

		if typ != "" && n.Type != typ {
			continue
		}

		if node != "" && k != node {
			continue
		}

		keys = append(keys, k)
	}

	return keys
}

// scopeFilter returns the "fleet" match condition for a scope, for
// collections (like nodes) that carry a fleet field directly. ok is false
// when the scope can never match (Empty), signaling the caller to skip the
// query entirely.
func scopeFilter(scope store.Scope) (filter bson.D, ok bool) {
	if scope.Empty() {
		return nil, false
	}

	if scope.All() {
		return bson.D{}, true
	}

	return bson.D{{Key: "fleet", Value: bson.D{{Key: "$in", Value: scope.Fleets()}}}}, true
}

func addDir(et *store.EntityTotal, dir string, bytes int64) {
	if dir == "up" {
		et.Up += bytes
	} else {
		et.Down += bytes
	}
}

// sortedTotals flattens an entity map into a list sorted by combined
// traffic descending, matching every "totals" ranking in sqlite/query.go.
func sortedTotals(m map[string]*store.EntityTotal) []store.EntityTotal {
	list := make([]store.EntityTotal, 0, len(m))
	for _, et := range m {
		list = append(list, *et)
	}

	sort.Slice(list, func(i, j int) bool { return list[i].Up+list[i].Down > list[j].Up+list[j].Down })

	return list
}

// top10 is sortedTotals capped to the ten biggest entities.
func top10(m map[string]*store.EntityTotal) []store.EntityTotal {
	list := sortedTotals(m)
	if len(list) > 10 {
		list = list[:10]
	}

	return list
}

// NodeStatuses returns every registered node with its health snapshot.
func (s *Store) NodeStatuses(ctx context.Context, scope store.Scope) ([]store.NodeStatus, error) {
	filter, ok := scopeFilter(scope)
	if !ok {
		return nil, nil
	}

	cur, err := s.nodes.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "fleet", Value: 1}, {Key: "id", Value: 1}}))
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	var out []store.NodeStatus

	for cur.Next(ctx) {
		var doc nodeDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out = append(out, store.NodeStatus{
			Node: doc.node(), LastErr: doc.LastErr, LastOK: doc.LastOK, LastErrTS: doc.LastErrTS,
			UptimeS: doc.UptimeS, NumGoroutine: doc.NumGoroutine,
			AllocBytes: doc.AllocBytes, SysBytes: doc.SysBytes, NumGC: doc.NumGC,
		})
	}

	return out, cur.Err()
}

// kindDirRow decodes a {kind?, entity/node, dir, bytes} aggregation group.
type kindDirRow struct {
	ID struct {
		Kind   string `bson:"kind"`
		Entity string `bson:"entity"`
		Dir    string `bson:"dir"`
	} `bson:"_id"`
	Bytes int64 `bson:"bytes"`
}

// NodeTotals returns per-entity traffic totals for one node over a range,
// keyed by kind. Unlike most queries here it does not scope to registered
// nodes - sqlite's equivalent query has no JOIN either, since a caller
// asking for one specific node_key's totals gets exactly that node's data
// whether or not it's still registered.
func (s *Store) NodeTotals(ctx context.Context, nodeKey string, from, to int64) (map[string][]store.EntityTotal, error) {
	match := bson.D{
		{Key: "node_key", Value: nodeKey},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}},
	}
	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "kind", Value: "$kind"}, {Key: "entity", Value: "$entity"}, {Key: "dir", Value: "$dir"}}},
		{Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}},
	}

	cur, err := s.trafficColl(from).Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	acc := map[string]map[string]*store.EntityTotal{}

	for cur.Next(ctx) {
		var row kindDirRow
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}

		if acc[row.ID.Kind] == nil {
			acc[row.ID.Kind] = map[string]*store.EntityTotal{}
		}

		et, ok := acc[row.ID.Kind][row.ID.Entity]
		if !ok {
			et = &store.EntityTotal{Entity: row.ID.Entity}
			acc[row.ID.Kind][row.ID.Entity] = et
		}

		addDir(et, row.ID.Dir, row.Bytes)
	}

	if err := cur.Err(); err != nil {
		return nil, err
	}

	out := map[string][]store.EntityTotal{}
	for kind, m := range acc {
		out[kind] = sortedTotals(m)
	}

	return out, nil
}

// Series returns dense arrays aligned to From + i*Step.
func (s *Store) Series(ctx context.Context, p store.SeriesParams) ([]store.Series, error) {
	if p.Step <= 0 {
		return nil, fmt.Errorf("%w: step must be positive", store.ErrBadQuery)
	}

	p.From -= p.From % p.Step
	if rem := p.To % p.Step; rem != 0 {
		p.To += p.Step - rem
	}

	slots := (p.To - p.From) / p.Step
	if slots <= 0 {
		return nil, fmt.Errorf("%w: empty range", store.ErrBadQuery)
	}

	if slots > store.MaxSlots {
		return nil, fmt.Errorf("%w: range/step yields %d slots (max %d)", store.ErrBadQuery, slots, store.MaxSlots)
	}

	if p.Kind == "online" {
		return s.onlineSeries(ctx, p, slots)
	}

	// Built up front so a bad agg is rejected whether or not any node matches.
	groupID, err := seriesGroupID(p.From, p.Step, p.Agg)
	if err != nil {
		return nil, err
	}

	return s.trafficSeries(ctx, p, slots, groupID)
}

// slotExpr builds the $floor((bucket-from)/step) expression shared by both
// series pipelines.
func slotExpr(from, step int64) bson.D {
	sub := bson.D{{Key: "$subtract", Value: bson.A{"$bucket", from}}}
	return bson.D{{Key: "$floor", Value: bson.D{{Key: "$divide", Value: bson.A{sub, step}}}}}
}

// seriesGroupID builds the $group _id for a traffic series pipeline: the
// slot plus whichever dimensions survive the requested aggregation mode.
func seriesGroupID(from, step int64, agg string) (bson.D, error) {
	slot := bson.E{Key: "slot", Value: slotExpr(from, step)}
	node := bson.E{Key: "node", Value: "$node_key"}
	entity := bson.E{Key: "entity", Value: "$entity"}
	dir := bson.E{Key: "dir", Value: "$dir"}

	switch agg {
	case store.AggNone, "":
		return bson.D{slot, node, entity, dir}, nil
	case store.AggEntity:
		return bson.D{slot, entity, dir}, nil
	case store.AggNode:
		return bson.D{slot, node, dir}, nil
	case store.AggTotal:
		return bson.D{slot, dir}, nil
	default:
		return nil, fmt.Errorf("%w: invalid agg %q", store.ErrBadQuery, agg)
	}
}

func (s *Store) trafficSeries(ctx context.Context, p store.SeriesParams, slots int64, groupID bson.D) ([]store.Series, error) {
	if p.Scope.Empty() {
		return nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	allowed := filterNodeKeys(nodes, p.Scope, p.Type, p.Node)
	if len(allowed) == 0 {
		return nil, nil
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: allowed}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: p.From}, {Key: "$lt", Value: p.To}}},
		{Key: "kind", Value: p.Kind},
	}

	if p.Entity != "" {
		match = append(match, bson.E{Key: "entity", Value: p.Entity})
	}

	if p.Dir != "" {
		match = append(match, bson.E{Key: "dir", Value: p.Dir})
	}

	group := bson.D{{Key: "_id", Value: groupID}, {Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}}}
	coll := s.trafficMinute

	if p.Step >= 3600 {
		coll = s.trafficHour
	}

	cur, err := coll.Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	return decodeTrafficSeries(ctx, cur, slots)
}

type trafficSeriesRow struct {
	ID struct {
		Slot   float64 `bson:"slot"`
		Node   string  `bson:"node"`
		Entity string  `bson:"entity"`
		Dir    string  `bson:"dir"`
	} `bson:"_id"`
	Bytes int64 `bson:"bytes"`
}

func decodeTrafficSeries(ctx context.Context, cur *mongo.Cursor, slots int64) ([]store.Series, error) {
	series := map[string]*store.Series{}

	for cur.Next(ctx) {
		var row trafficSeriesRow
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}

		key := row.ID.Node + "\x00" + row.ID.Entity + "\x00" + row.ID.Dir

		sr, ok := series[key]
		if !ok {
			sr = &store.Series{Node: row.ID.Node, Entity: row.ID.Entity, Dir: row.ID.Dir, Points: make([]int64, slots)}
			series[key] = sr
		}

		if slot := roundSlot(row.ID.Slot); slot >= 0 && slot < slots {
			sr.Points[slot] += row.Bytes
		}
	}

	if err := cur.Err(); err != nil {
		return nil, err
	}

	return orderedSeries(series), nil
}

// roundSlot converts the $floor'd slot back to int64. Bucket/from/step are
// always exact multiples so the float is already integral; rounding just
// guards against floating-point noise.
func roundSlot(f float64) int64 {
	return int64(f + 0.5)
}

// orderedSeries sorts by the series map key, matching sqlite's
// sort.Strings(order) over the same "node\x00entity\x00dir" (or bare node,
// for online series) keys.
func orderedSeries(series map[string]*store.Series) []store.Series {
	keys := make([]string, 0, len(series))
	for k := range series {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	out := make([]store.Series, 0, len(keys))
	for _, k := range keys {
		out = append(out, *series[k])
	}

	return out
}

func (s *Store) onlineSeries(ctx context.Context, p store.SeriesParams, slots int64) ([]store.Series, error) {
	if p.Scope.Empty() {
		return nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	allowed := filterNodeKeys(nodes, p.Scope, p.Type, p.Node)
	if len(allowed) == 0 {
		return nil, nil
	}

	coll := s.onlineMinute
	if p.Step >= 3600 {
		coll = s.onlineHour
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: allowed}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: p.From}, {Key: "$lt", Value: p.To}}},
	}
	// Per-slot: MAX per node (it's a gauge), then SUM across nodes when the
	// caller wants nodes collapsed.
	perNodeGroup := bson.D{
		{Key: "_id", Value: bson.D{{Key: "slot", Value: slotExpr(p.From, p.Step)}, {Key: "node", Value: "$node_key"}}},
		{Key: "count", Value: bson.D{{Key: "$max", Value: "$count"}}},
	}

	pipeline := mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: perNodeGroup}}}

	perNode := p.Agg == store.AggNone || p.Agg == store.AggNode || p.Agg == ""
	if !perNode {
		collapse := bson.D{
			{Key: "_id", Value: bson.D{{Key: "slot", Value: "$_id.slot"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: "$count"}}},
		}
		pipeline = append(pipeline, bson.D{{Key: "$group", Value: collapse}})
	}

	cur, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	return decodeOnlineSeries(ctx, cur, slots)
}

type onlineSeriesRow struct {
	ID struct {
		Slot float64 `bson:"slot"`
		Node string  `bson:"node"`
	} `bson:"_id"`
	Count int64 `bson:"count"`
}

func decodeOnlineSeries(ctx context.Context, cur *mongo.Cursor, slots int64) ([]store.Series, error) {
	series := map[string]*store.Series{}

	for cur.Next(ctx) {
		var row onlineSeriesRow
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}

		sr, ok := series[row.ID.Node]
		if !ok {
			sr = &store.Series{Node: row.ID.Node, Points: make([]int64, slots)}
			series[row.ID.Node] = sr
		}

		if slot := roundSlot(row.ID.Slot); slot >= 0 && slot < slots {
			sr.Points[slot] = row.Count
		}
	}

	if err := cur.Err(); err != nil {
		return nil, err
	}

	return orderedSeries(series), nil
}

// OverviewTraffic aggregates hub-inbound traffic (totals, per fleet, top
// users, top nodes) plus the distinct online-user count, all computed inside
// scope: hubKeys/allKeys restrict every aggregation's $match, so totals and
// top-N never see another fleet's data to filter out afterwards.
func (s *Store) OverviewTraffic(ctx context.Context, from, to int64, scope store.Scope) (*store.OverviewTraffic, error) {
	ov := &store.OverviewTraffic{}

	if scope.Empty() {
		return ov, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	coll := s.trafficColl(from)
	hubKeys := filterNodeKeys(nodes, scope, "hub", "")
	allKeys := filterNodeKeys(nodes, scope, "", "")

	if ov.Totals, ov.FleetBytes, err = s.fleetTotals(ctx, coll, nodes, hubKeys, from, to); err != nil {
		return nil, err
	}

	if ov.TopUsers, err = s.topEntityTotals(ctx, coll, hubKeys, "user", "entity", from, to); err != nil {
		return nil, err
	}

	if ov.TopNodes, err = s.topEntityTotals(ctx, coll, allKeys, "inbound", "node_key", from, to); err != nil {
		return nil, err
	}

	online, err := s.onlineEmailSet(ctx, hubKeys)
	if err != nil {
		return nil, err
	}

	ov.OnlineUsers = int64(len(online))

	return ov, nil
}

type nodeDirRow struct {
	ID struct {
		Node string `bson:"node"`
		Dir  string `bson:"dir"`
	} `bson:"_id"`
	Bytes int64 `bson:"bytes"`
}

// fleetTotals sums hub-inbound traffic per fleet and overall.
func (s *Store) fleetTotals(
	ctx context.Context, coll *mongo.Collection, nodes map[string]store.Node, hubKeys []string, from, to int64,
) (store.DirTotal, map[string]store.DirTotal, error) {
	var totals store.DirTotal

	fleets := map[string]store.DirTotal{}

	if len(hubKeys) == 0 {
		return totals, fleets, nil
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: hubKeys}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}},
		{Key: "kind", Value: "inbound"},
	}
	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "node", Value: "$node_key"}, {Key: "dir", Value: "$dir"}}},
		{Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}},
	}

	cur, err := coll.Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return totals, nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	for cur.Next(ctx) {
		var row nodeDirRow
		if err := cur.Decode(&row); err != nil {
			return totals, nil, err
		}

		fleet := nodes[row.ID.Node].Fleet
		ft := fleets[fleet]

		if row.ID.Dir == "up" {
			ft.Up += row.Bytes
			totals.Up += row.Bytes
		} else {
			ft.Down += row.Bytes
			totals.Down += row.Bytes
		}

		fleets[fleet] = ft
	}

	return totals, fleets, cur.Err()
}

// topEntityTotals groups by (entityField, dir) and returns the ten biggest
// entities by combined traffic - the shared shape behind TopUsers (entity
// field "entity") and TopNodes (entity field "node_key").
func (s *Store) topEntityTotals(
	ctx context.Context, coll *mongo.Collection, keys []string, kind, entityField string, from, to int64,
) ([]store.EntityTotal, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: keys}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}},
		{Key: "kind", Value: kind},
	}
	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "entity", Value: "$" + entityField}, {Key: "dir", Value: "$dir"}}},
		{Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}},
	}

	cur, err := coll.Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	m := map[string]*store.EntityTotal{}

	for cur.Next(ctx) {
		var row kindDirRow
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}

		et, ok := m[row.ID.Entity]
		if !ok {
			et = &store.EntityTotal{Entity: row.ID.Entity}
			m[row.ID.Entity] = et
		}

		addDir(et, row.ID.Dir, row.Bytes)
	}

	if err := cur.Err(); err != nil {
		return nil, err
	}

	return top10(m), nil
}

// onlineEmailSet returns the distinct emails currently online on any of the
// given nodes.
func (s *Store) onlineEmailSet(ctx context.Context, keys []string) (map[string]bool, error) {
	out := map[string]bool{}

	if len(keys) == 0 {
		return out, nil
	}

	filter := bson.D{{Key: "node_key", Value: bson.D{{Key: "$in", Value: keys}}}}

	cur, err := s.onlineCurrent.Find(ctx, filter, options.Find().SetProjection(bson.D{{Key: "email", Value: 1}}))
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	for cur.Next(ctx) {
		var doc struct {
			Email string `bson:"email"`
		}

		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out[doc.Email] = true
	}

	return out, cur.Err()
}

// Users lists per-user traffic over [from,to) - hub nodes only.
// Hubs reflect traffic/online presence in [seenFrom,to).
func (s *Store) Users(ctx context.Context, from, to, seenFrom int64, scope store.Scope) ([]store.UserRow, error) {
	if scope.Empty() {
		return nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	hubKeys := filterNodeKeys(nodes, scope, "hub", "")
	if len(hubKeys) == 0 {
		return nil, nil
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: hubKeys}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}},
		{Key: "kind", Value: "user"},
	}
	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "entity", Value: "$entity"}, {Key: "dir", Value: "$dir"}}},
		{Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}},
	}

	cur, err := s.trafficColl(from).Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return nil, err
	}

	users, err := decodeUserTotals(ctx, cur)
	_ = cur.Close(ctx)

	if err != nil {
		return nil, err
	}

	hubsByEmail, err := s.hubsSeen(ctx, nodes, seenFrom, to, scope, "")
	if err != nil {
		return nil, err
	}

	// Scoped: presence on a fleet the caller cannot see must not surface as an
	// "online" flag on a user it can.
	scopedHubKeys := filterNodeKeys(nodes, scope, "hub", "")

	online, err := s.onlineEmailSet(ctx, scopedHubKeys)
	if err != nil {
		return nil, err
	}

	return finalizeUserRows(users, hubsByEmail, online), nil
}

type userTotalRow struct {
	ID struct {
		Entity string `bson:"entity"`
		Dir    string `bson:"dir"`
	} `bson:"_id"`
	Bytes int64 `bson:"bytes"`
}

func decodeUserTotals(ctx context.Context, cur *mongo.Cursor) (map[string]*store.UserRow, error) {
	users := map[string]*store.UserRow{}

	for cur.Next(ctx) {
		var row userTotalRow
		if err := cur.Decode(&row); err != nil {
			return nil, err
		}

		u, ok := users[row.ID.Entity]
		if !ok {
			u = &store.UserRow{Email: row.ID.Entity}
			users[row.ID.Entity] = u
		}

		if row.ID.Dir == "up" {
			u.Up += row.Bytes
		} else {
			u.Down += row.Bytes
		}
	}

	return users, cur.Err()
}

type hubSeenRow struct {
	ID struct {
		Entity string `bson:"entity"`
		Node   string `bson:"node"`
	} `bson:"_id"`
	LastSeen int64 `bson:"last_seen"`
}

func (s *Store) hubsSeen(
	ctx context.Context, nodes map[string]store.Node, seenFrom, to int64, scope store.Scope, emailFilter string,
) (map[string][]store.UserHubSeen, error) {
	hubKeys := filterNodeKeys(nodes, scope, "hub", "")
	if len(hubKeys) == 0 {
		return map[string][]store.UserHubSeen{}, nil
	}

	last := map[string]map[string]int64{}

	set := func(email, node string, ts int64) {
		if last[email] == nil {
			last[email] = map[string]int64{}
		}

		if ts > last[email][node] {
			last[email][node] = ts
		}
	}

	match := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: hubKeys}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: seenFrom}, {Key: "$lt", Value: to}}},
		{Key: "kind", Value: "user"},
	}
	if emailFilter != "" {
		match = append(match, bson.E{Key: "entity", Value: emailFilter})
	}

	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "entity", Value: "$entity"}, {Key: "node", Value: "$node_key"}}},
		{Key: "last_seen", Value: bson.D{{Key: "$max", Value: "$bucket"}}},
	}

	cur, err := s.trafficColl(seenFrom).Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return nil, err
	}

	for cur.Next(ctx) {
		var row hubSeenRow
		if err := cur.Decode(&row); err != nil {
			_ = cur.Close(ctx)
			return nil, err
		}

		set(row.ID.Entity, row.ID.Node, row.LastSeen)
	}

	if err := cur.Err(); err != nil {
		_ = cur.Close(ctx)
		return nil, err
	}

	_ = cur.Close(ctx)

	onlineFilter := bson.D{
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: hubKeys}}},
		{Key: "last_seen", Value: bson.D{{Key: "$gte", Value: seenFrom}, {Key: "$lt", Value: to}}},
	}
	if emailFilter != "" {
		onlineFilter = append(onlineFilter, bson.E{Key: "email", Value: emailFilter})
	}

	onlineGroup := bson.D{
		{Key: "_id", Value: bson.D{{Key: "entity", Value: "$email"}, {Key: "node", Value: "$node_key"}}},
		{Key: "last_seen", Value: bson.D{{Key: "$max", Value: "$last_seen"}}},
	}

	ocur, err := s.onlineCurrent.Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: onlineFilter}}, {{Key: "$group", Value: onlineGroup}}})
	if err != nil {
		return nil, err
	}

	defer func() { _ = ocur.Close(ctx) }()

	for ocur.Next(ctx) {
		var row hubSeenRow
		if err := ocur.Decode(&row); err != nil {
			return nil, err
		}

		set(row.ID.Entity, row.ID.Node, row.LastSeen)
	}

	if err := ocur.Err(); err != nil {
		return nil, err
	}

	out := make(map[string][]store.UserHubSeen, len(last))
	for email, byNode := range last {
		list := make([]store.UserHubSeen, 0, len(byNode))
		for node, ts := range byNode {
			list = append(list, store.UserHubSeen{Node: node, LastSeen: ts})
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

func finalizeUserRows(
	users map[string]*store.UserRow, hubsByEmail map[string][]store.UserHubSeen, online map[string]bool,
) []store.UserRow {
	out := make([]store.UserRow, 0, len(users))

	for email, u := range users {
		u.Hubs = hubsByEmail[email]
		if u.Hubs == nil {
			u.Hubs = []store.UserHubSeen{}
		}

		u.OnlineNow = online[email]
		out = append(out, *u)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Up+out[i].Down > out[j].Up+out[j].Down })

	return out
}

// UserDetail returns totals, per-node breakdown, recently-seen hubs, and IPs.
// The node_key filter (any type - a pseudo-identity's detail can live on an
// exit) is what keeps the totals/perNode aggregate inside scope.
func (s *Store) UserDetail(
	ctx context.Context, email string, from, to, seenFrom int64, scope store.Scope,
) (store.DirTotal, []store.EntityTotal, []store.UserHubSeen, []store.OnlineIPRow, error) {
	if scope.Empty() {
		return store.DirTotal{}, nil, nil, nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return store.DirTotal{}, nil, nil, nil, err
	}

	allowed := filterNodeKeys(nodes, scope, "", "")
	if len(allowed) == 0 {
		return store.DirTotal{}, nil, nil, nil, nil
	}

	match := bson.D{
		{Key: "kind", Value: "user"}, {Key: "entity", Value: email},
		{Key: "node_key", Value: bson.D{{Key: "$in", Value: allowed}}},
		{Key: "bucket", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}},
	}
	group := bson.D{
		{Key: "_id", Value: bson.D{{Key: "node", Value: "$node_key"}, {Key: "dir", Value: "$dir"}}},
		{Key: "bytes", Value: bson.D{{Key: "$sum", Value: "$bytes"}}},
	}

	cur, err := s.trafficColl(from).Aggregate(ctx, mongo.Pipeline{{{Key: "$match", Value: match}}, {{Key: "$group", Value: group}}})
	if err != nil {
		return store.DirTotal{}, nil, nil, nil, err
	}

	total, perNode, err := decodeUserDetailTotals(ctx, cur)
	_ = cur.Close(ctx)

	if err != nil {
		return store.DirTotal{}, nil, nil, nil, err
	}

	hubsByEmail, err := s.hubsSeen(ctx, nodes, seenFrom, to, scope, email)
	if err != nil {
		return store.DirTotal{}, nil, nil, nil, err
	}

	seen := hubsByEmail[email]
	if seen == nil {
		seen = []store.UserHubSeen{}
	}

	ips, err := s.userIPRows(ctx, email)
	if err != nil {
		return store.DirTotal{}, nil, nil, nil, err
	}

	return total, sortedTotals(perNode), seen, ips, nil
}

func decodeUserDetailTotals(ctx context.Context, cur *mongo.Cursor) (store.DirTotal, map[string]*store.EntityTotal, error) {
	var total store.DirTotal

	perNode := map[string]*store.EntityTotal{}

	for cur.Next(ctx) {
		var row nodeDirRow
		if err := cur.Decode(&row); err != nil {
			return store.DirTotal{}, nil, err
		}

		et, ok := perNode[row.ID.Node]
		if !ok {
			et = &store.EntityTotal{Entity: row.ID.Node}
			perNode[row.ID.Node] = et
		}

		addDir(et, row.ID.Dir, row.Bytes)

		if row.ID.Dir == "up" {
			total.Up += row.Bytes
		} else {
			total.Down += row.Bytes
		}
	}

	return total, perNode, cur.Err()
}

func (s *Store) userIPRows(ctx context.Context, email string) ([]store.OnlineIPRow, error) {
	filter := bson.D{{Key: "email", Value: email}}
	opts := options.Find().SetSort(bson.D{{Key: "last_seen", Value: -1}})

	cur, err := s.onlineCurrent.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	var out []store.OnlineIPRow

	for cur.Next(ctx) {
		var doc onlineCurrentDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out = append(out, store.OnlineIPRow{Node: doc.NodeKey, IP: doc.IP, LastSeen: doc.LastSeen})
	}

	return out, cur.Err()
}

// OnlineNow returns the current online snapshot grouped by (node, email).
// online_current has no fleet field of its own, so scoping goes through the
// registered-node key set like every other non-nodes-collection query here.
func (s *Store) OnlineNow(ctx context.Context, scope store.Scope) ([]store.OnlineRow, error) {
	if scope.Empty() {
		return nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	allowed := filterNodeKeys(nodes, scope, "", "")
	if len(allowed) == 0 {
		return nil, nil
	}

	filter := bson.D{{Key: "node_key", Value: bson.D{{Key: "$in", Value: allowed}}}}
	byNodeEmailSeen := bson.D{{Key: "node_key", Value: 1}, {Key: "email", Value: 1}, {Key: "last_seen", Value: -1}}

	cur, err := s.onlineCurrent.Find(ctx, filter, options.Find().SetSort(byNodeEmailSeen))
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	var out []store.OnlineRow

	for cur.Next(ctx) {
		var doc onlineCurrentDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		if len(out) == 0 || out[len(out)-1].Node != doc.NodeKey || out[len(out)-1].Email != doc.Email {
			out = append(out, store.OnlineRow{Node: doc.NodeKey, Email: doc.Email})
		}

		if doc.IP != "" {
			last := &out[len(out)-1]
			last.IPs = append(last.IPs, store.OnlineIPRow{Node: doc.NodeKey, IP: doc.IP, LastSeen: doc.LastSeen})
		}
	}

	return out, cur.Err()
}

// Counters returns all current cumulative counters, ordered by
// (node_key, name). Scoped the same way as OnlineNow: counters_last has no
// fleet field, so the registered-node key set carries the restriction.
func (s *Store) Counters(ctx context.Context, scope store.Scope) ([]store.CounterRow, error) {
	if scope.Empty() {
		return nil, nil
	}

	nodes, err := s.loadNodes(ctx)
	if err != nil {
		return nil, err
	}

	allowed := filterNodeKeys(nodes, scope, "", "")
	if len(allowed) == 0 {
		return nil, nil
	}

	filter := bson.D{{Key: "node_key", Value: bson.D{{Key: "$in", Value: allowed}}}}
	opts := options.Find().SetSort(bson.D{{Key: "node_key", Value: 1}, {Key: "name", Value: 1}})

	cur, err := s.countersLast.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	var out []store.CounterRow

	for cur.Next(ctx) {
		var doc counterDoc
		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out = append(out, store.CounterRow{NodeKey: doc.NodeKey, Name: doc.Name, Value: doc.Value})
	}

	return out, cur.Err()
}
