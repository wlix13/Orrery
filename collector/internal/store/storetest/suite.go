// Package storetest is the conformance suite every storage backend must
// pass - behavior differences between backends are bugs, not features.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/xray"
)

type (
	Node         = store.Node
	Sample       = store.Sample
	Delta        = store.Delta
	SeriesParams = store.SeriesParams
)

const (
	AggEntity = store.AggEntity
	AggTotal  = store.AggTotal
)

// Factory returns a fresh, empty store; cleanup registers via t.Cleanup.
type Factory func(t *testing.T) store.Store

// Run executes the full conformance suite against a backend.
func Run(t *testing.T, open Factory) {
	t.Helper()

	tests := []struct {
		name string
		fn   func(*testing.T, store.Store)
	}{
		{"SeriesMinuteAndAgg", testSeriesMinuteAndAgg},
		{"SeriesHourTable", testSeriesHourTable},
		{"SeriesRejectsBadParams", testSeriesRejectsBadParams},
		{"OnlineLifecycle", testOnlineLifecycle},
		{"UsersHubOnly", testUsersHubOnly},
		{"RegisterNodesPrunes", testRegisterNodesPrunes},
		{"Retention", testRetention},
		{"ScopeIsolatesLists", testScopeIsolatesLists},
		{"ScopeIsolatesAggregates", testScopeIsolatesAggregates},
		{"ScopeZeroValueMatchesNothing", testScopeZeroValueMatchesNothing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn(t, open(t))
		})
	}
}

var testNodes = []Node{
	{Key: "main/mskA00", Fleet: "main", ID: "mskA00", Region: "msk", Type: "hub", Hostname: "mskA00.example", Collect: "full"},
	{Key: "main/nlA00", Fleet: "main", ID: "nlA00", Region: "nl", Type: "exit", Hostname: "nlA00.example", Collect: "traffic"},
}

// now is aligned so minute/hour bucketing in assertions stays simple.
var now = time.Now().Truncate(time.Hour).Add(-time.Hour)

func seed(t *testing.T, s store.Store) {
	t.Helper()

	ctx := context.Background()
	if err := s.RegisterNodes(ctx, testNodes); err != nil {
		t.Fatal(err)
	}
	// Two polls a minute apart on the hub, one on the exit.
	polls := []Sample{
		{
			NodeKey: "main/mskA00", TS: now,
			Counters: map[string]int64{"c1": 100},
			Deltas: []Delta{
				{Kind: "inbound", Entity: "direct-xhttp", Dir: "up", Bytes: 100},
				{Kind: "inbound", Entity: "direct-xhttp", Dir: "down", Bytes: 900},
				{Kind: "user", Entity: "alice@ns", Dir: "down", Bytes: 700},
			},
			Sys:             &xray.SysStats{UptimeS: 3600, NumGoroutine: 50, Alloc: 1 << 20, Sys: 1 << 22, NumGC: 3},
			Online:          []xray.OnlineUser{{Email: "alice@ns", IPs: []xray.OnlineIP{{IP: "198.51.100.7", LastSeen: now.Unix()}}}},
			OnlineCollected: true,
		},
		{
			NodeKey: "main/mskA00", TS: now.Add(time.Minute),
			Counters: map[string]int64{"c1": 150},
			Deltas: []Delta{
				{Kind: "inbound", Entity: "direct-xhttp", Dir: "up", Bytes: 50},
				{Kind: "user", Entity: "alice@ns", Dir: "down", Bytes: 300},
			},
			OnlineCollected: true, // alice went offline
		},
		{
			NodeKey: "main/nlA00", TS: now,
			Counters: map[string]int64{"c2": 10},
			Deltas: []Delta{
				{Kind: "inbound", Entity: "direct-xhttp", Dir: "down", Bytes: 5000},
				{Kind: "user", Entity: "mskA00-nlA00@ns", Dir: "down", Bytes: 5000},
			},
		},
	}
	for _, p := range polls {
		if err := s.WriteSample(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
}

func testSeriesMinuteAndAgg(t *testing.T, s store.Store) {
	seed(t, s)

	ctx := context.Background()

	from := now.Unix()
	to := now.Add(10 * time.Minute).Unix()

	// Per-entity minute series on the hub.
	series, err := s.Series(ctx, SeriesParams{
		From: from, To: to, Step: 60, Kind: "inbound", Node: "main/mskA00", Scope: store.AllFleets(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Series identity is (node, entity, dir): both up-polls land in one
	// series' points, so we get exactly (up) and (down).
	if len(series) != 2 {
		t.Fatalf("series count = %d (%+v), want 2", len(series), series)
	}

	var upPoints []int64

	for _, sr := range series {
		if sr.Dir == "up" && sr.Entity == "direct-xhttp" {
			upPoints = sr.Points
		}
	}

	if len(upPoints) != 10 || upPoints[0] != 100 || upPoints[1] != 50 {
		t.Errorf("up points = %v, want [100 50 0 ...]", upPoints)
	}

	// Fleet-wide total, hub only (excludes the exit's 5000).
	series, err = s.Series(ctx, SeriesParams{
		From: from, To: to, Step: 60, Kind: "inbound", Type: "hub", Agg: AggTotal, Dir: "down", Scope: store.AllFleets(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(series) != 1 || series[0].Points[0] != 900 {
		t.Fatalf("hub-down total = %+v, want one series starting [900 ...]", series)
	}
}

func testSeriesHourTable(t *testing.T, s store.Store) {
	seed(t, s)

	from := now.Truncate(time.Hour).Unix()
	to := now.Truncate(time.Hour).Add(2 * time.Hour).Unix()

	series, err := s.Series(context.Background(), SeriesParams{
		From: from, To: to, Step: 3600, Kind: "user", Agg: AggEntity, Entity: "alice@ns", Scope: store.AllFleets(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Both minute-polls land in the same hour bucket: 700+300.
	if len(series) != 1 || series[0].Points[0] != 1000 {
		t.Fatalf("hour series = %+v, want [1000 0]", series)
	}
}

// Rejected parameters must be tagged ErrBadQuery: the API echoes only those.
func testSeriesRejectsBadParams(t *testing.T, s store.Store) {
	from := now.Unix()

	bad := map[string]SeriesParams{
		"empty range": {From: from, To: from, Step: 60, Kind: "inbound", Scope: store.AllFleets()},
		"too wide":    {From: from, To: from + (store.MaxSlots+1)*60, Step: 60, Kind: "inbound", Scope: store.AllFleets()},
		"invalid agg": {From: from, To: from + 600, Step: 60, Kind: "inbound", Agg: "sideways", Scope: store.AllFleets()},
		"zero step":   {From: from, To: from + 600, Step: 0, Kind: "inbound", Scope: store.AllFleets()},
	}

	for name, p := range bad {
		_, err := s.Series(context.Background(), p)
		if !errors.Is(err, store.ErrBadQuery) {
			t.Errorf("%s: err = %v, want ErrBadQuery", name, err)
		}
	}
}

func testOnlineLifecycle(t *testing.T, s store.Store) {
	seed(t, s)

	ctx := context.Background()

	// Second poll reported nobody online → snapshot must be empty.
	rows, err := s.OnlineNow(ctx, store.AllFleets())
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 0 {
		t.Fatalf("online rows = %+v, want none", rows)
	}

	// But the online gauge kept the minute-1 count.
	series, err := s.Series(ctx, SeriesParams{
		From: now.Unix(), To: now.Add(2 * time.Minute).Unix(), Step: 60, Kind: "online", Agg: AggTotal, Scope: store.AllFleets(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(series) != 1 || series[0].Points[0] != 1 || series[0].Points[1] != 0 {
		t.Fatalf("online series = %+v, want [1 0]", series)
	}
}

func testUsersHubOnly(t *testing.T, s store.Store) {
	seed(t, s)

	from, to := now.Unix(), now.Add(time.Hour).Unix()

	users, err := s.Users(context.Background(), from, to, from, store.AllFleets())
	if err != nil {
		t.Fatal(err)
	}
	// The exit's per-hub pseudo-user (mskA00-nlA00@ns) must not appear.
	if len(users) != 1 || users[0].Email != "alice@ns" {
		t.Fatalf("users = %+v, want only alice@ns", users)
	}

	if users[0].Down != 1000 || users[0].Up != 0 {
		t.Errorf("alice totals = up %d down %d, want up 0 down 1000", users[0].Up, users[0].Down)
	}

	if len(users[0].Hubs) != 1 || users[0].Hubs[0].Node != "main/mskA00" {
		t.Errorf("alice hubs = %v", users[0].Hubs)
	}
}

func testRegisterNodesPrunes(t *testing.T, s store.Store) {
	seed(t, s)

	ctx := context.Background()

	if err := s.RegisterNodes(ctx, testNodes[:1]); err != nil { // drop the exit
		t.Fatal(err)
	}

	statuses, err := s.NodeStatuses(ctx, store.AllFleets())
	if err != nil {
		t.Fatal(err)
	}

	if len(statuses) != 1 || statuses[0].Key != "main/mskA00" {
		t.Fatalf("statuses = %+v, want only main/mskA00", statuses)
	}

	last, err := s.LastCounters(ctx, "main/nlA00")
	if err != nil {
		t.Fatal(err)
	}

	if len(last) != 0 {
		t.Errorf("pruned node still has counters: %v", last)
	}
}

func testRetention(t *testing.T, s store.Store) {
	ctx := context.Background()

	if err := s.RegisterNodes(ctx, testNodes); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-100 * 24 * time.Hour)
	if err := s.WriteSample(ctx, Sample{
		NodeKey: "main/mskA00", TS: old,
		Counters: map[string]int64{"c": 1},
		Deltas:   []Delta{{Kind: "inbound", Entity: "t", Dir: "up", Bytes: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Retention(ctx, 72*time.Hour, 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	series, err := s.Series(ctx, SeriesParams{
		From: old.Add(-time.Hour).Truncate(time.Hour).Unix(),
		To:   old.Add(time.Hour).Truncate(time.Hour).Unix(),
		Step: 3600, Kind: "inbound", Agg: AggTotal,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, sr := range series {
		for _, p := range sr.Points {
			if p != 0 {
				t.Fatalf("expected 100-day-old hour data purged, got %+v", series)
			}
		}
	}
}

// Two fleets, so scoping has something to exclude.
var scopedNodes = []Node{
	{Key: "main/hub01", Fleet: "main", ID: "hub01", Region: "eu", Type: "hub", Hostname: "hub01.example", Collect: "full"},
	{Key: "other/hub01", Fleet: "other", ID: "hub01", Region: "us", Type: "hub", Hostname: "hub01.other.example", Collect: "full"},
}

var scopedWindow = struct{ from, to int64 }{now.Unix(), now.Add(time.Hour).Unix()}

// seedScoped gives each fleet traffic and an online user, and adds roamer@ns:
// traffic on main but presence only on the fleet a main-scoped caller cannot
// see. The other fleet's numbers are larger, so an unscoped top-N would lead
// with them.
func seedScoped(t *testing.T, s store.Store) {
	t.Helper()

	ctx := context.Background()
	if err := s.RegisterNodes(ctx, scopedNodes); err != nil {
		t.Fatal(err)
	}

	samples := []Sample{
		{
			NodeKey: "main/hub01", TS: now,
			Counters: map[string]int64{"c": 1000},
			Deltas: []Delta{
				{Kind: "inbound", Entity: "in", Dir: "down", Bytes: 1000},
				{Kind: "user", Entity: "u@main", Dir: "down", Bytes: 1000},
				{Kind: "user", Entity: "roamer@ns", Dir: "down", Bytes: 10},
			},
			Online:          []xray.OnlineUser{{Email: "u@main", IPs: []xray.OnlineIP{{IP: "192.0.2.1", LastSeen: now.Unix()}}}},
			OnlineCollected: true,
		},
		{
			NodeKey: "other/hub01", TS: now,
			Counters: map[string]int64{"c": 9000},
			Deltas: []Delta{
				{Kind: "inbound", Entity: "in", Dir: "down", Bytes: 9000},
				{Kind: "user", Entity: "u@other", Dir: "down", Bytes: 9000},
			},
			Online: []xray.OnlineUser{
				{Email: "u@other", IPs: []xray.OnlineIP{{IP: "192.0.2.2", LastSeen: now.Unix()}}},
				{Email: "roamer@ns", IPs: []xray.OnlineIP{{IP: "192.0.2.3", LastSeen: now.Unix()}}},
			},
			OnlineCollected: true,
		},
	}

	for _, smp := range samples {
		if err := s.WriteSample(ctx, smp); err != nil {
			t.Fatal(err)
		}
	}
}

func testScopeIsolatesLists(t *testing.T, s store.Store) {
	seedScoped(t, s)

	ctx := context.Background()
	main := store.FleetScope("main")

	statuses, err := s.NodeStatuses(ctx, main)
	if err != nil {
		t.Fatal(err)
	}

	if len(statuses) != 1 || statuses[0].Key != "main/hub01" {
		t.Errorf("NodeStatuses = %+v, want only main/hub01", statuses)
	}

	online, err := s.OnlineNow(ctx, main)
	if err != nil {
		t.Fatal(err)
	}

	if len(online) != 1 || online[0].Node != "main/hub01" {
		t.Errorf("OnlineNow = %+v, want only main/hub01", online)
	}

	counters, err := s.Counters(ctx, main)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range counters {
		if c.NodeKey != "main/hub01" {
			t.Errorf("Counters leaked %s", c.NodeKey)
		}
	}

	users, err := s.Users(ctx, scopedWindow.from, scopedWindow.to, scopedWindow.from, main)
	if err != nil {
		t.Fatal(err)
	}

	byEmail := map[string]store.UserRow{}
	for _, u := range users {
		byEmail[u.Email] = u
	}

	if _, ok := byEmail["u@other"]; ok {
		t.Errorf("Users leaked the other fleet's user: %+v", users)
	}

	roamer, ok := byEmail["roamer@ns"]
	if !ok {
		t.Fatalf("Users = %+v, want roamer@ns (it has traffic on main)", users)
	}

	if roamer.OnlineNow {
		t.Error("online flag leaked presence from a fleet outside the scope")
	}
}

// testScopeIsolatesAggregates is the case post-filtering would fail: totals and
// top-N must be computed inside the scope, not trimmed afterwards.
func testScopeIsolatesAggregates(t *testing.T, s store.Store) {
	seedScoped(t, s)

	ctx := context.Background()
	main := store.FleetScope("main")

	ov, err := s.OverviewTraffic(ctx, scopedWindow.from, scopedWindow.to, main)
	if err != nil {
		t.Fatal(err)
	}

	if ov.Totals.Down != 1000 {
		t.Errorf("scoped totals = %d, want 1000 (the other fleet's 9000 excluded)", ov.Totals.Down)
	}

	if _, ok := ov.FleetBytes["other"]; ok {
		t.Errorf("FleetBytes leaked the other fleet: %+v", ov.FleetBytes)
	}

	for _, u := range ov.TopUsers {
		if u.Entity == "u@other" {
			t.Errorf("TopUsers leaked %s from outside the scope", u.Entity)
		}
	}

	for _, n := range ov.TopNodes {
		if n.Entity != "main/hub01" {
			t.Errorf("TopNodes leaked %s", n.Entity)
		}
	}

	series, err := s.Series(ctx, SeriesParams{
		From: scopedWindow.from, To: scopedWindow.to, Step: 60,
		Kind: "inbound", Agg: AggTotal, Scope: main,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(series) != 1 || series[0].Points[0] != 1000 {
		t.Errorf("scoped series = %+v, want one series starting 1000", series)
	}
}

// The zero Scope matches nothing, so a caller that forgets it sees no data.
func testScopeZeroValueMatchesNothing(t *testing.T, s store.Store) {
	seedScoped(t, s)

	ctx := context.Background()

	nodes, err := s.NodeStatuses(ctx, store.Scope{})
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 0 {
		t.Errorf("zero Scope returned %d nodes, want none", len(nodes))
	}

	ov, err := s.OverviewTraffic(ctx, scopedWindow.from, scopedWindow.to, store.Scope{})
	if err != nil {
		t.Fatal(err)
	}

	if ov.Totals.Down != 0 || len(ov.TopNodes) != 0 {
		t.Errorf("zero Scope returned traffic: %+v", ov)
	}
}
