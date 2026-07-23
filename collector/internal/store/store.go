// Package store defines the storage contract and shared data types.
//
// Implementations live in subpackages: sqlite (default, embedded) and
// mongo (optional, selected by a mongodb:// URI). Both are exercised by
// the storetest conformance suite - behavior differences are bugs.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/wlix13/orrery/collector/internal/xray"
)

// ErrBadQuery marks query parameters a backend refuses; the API echoes only these.
var ErrBadQuery = errors.New("bad query")

// Scope limits a read to a set of fleets. The zero value matches nothing, so
// a caller that forgets to set it sees no data rather than all of it; use
// AllFleets or FleetScope to construct one.
type Scope struct {
	fleets []string
	all    bool
}

// AllFleets is an unrestricted scope.
func AllFleets() Scope { return Scope{all: true} }

// FleetScope limits a read to the named fleets.
func FleetScope(fleets ...string) Scope { return Scope{fleets: fleets} }

// All reports whether the scope is unrestricted.
func (s Scope) All() bool { return s.all }

// Fleets lists the permitted fleets; empty when All is true.
func (s Scope) Fleets() []string { return s.fleets }

// Empty reports whether the scope can never match, which lets a backend skip
// the query entirely.
func (s Scope) Empty() bool { return !s.all && len(s.fleets) == 0 }

// Permits reports whether a fleet is inside the scope.
func (s Scope) Permits(fleet string) bool {
	if s.all {
		return true
	}

	for _, f := range s.fleets {
		if f == fleet {
			return true
		}
	}

	return false
}

// Store is the full storage contract: ingest (poller), queries (API),
// and lifecycle (main).
type Store interface {
	// RegisterNodes reconciles the node registry with the configured set.
	// Rows for removed nodes are deleted; their traffic history becomes
	// invisible (every query is scoped to registered nodes).
	RegisterNodes(ctx context.Context, nodes []Node) error
	// LastCounters loads the delta base for a node (once per poller start).
	LastCounters(ctx context.Context, nodeKey string) (map[string]int64, error)
	// WriteSample persists one poll: cumulative counters, traffic deltas
	// into minute+hour buckets, node health, and the online snapshot.
	WriteSample(ctx context.Context, smp Sample) error
	// MarkNodeError records a failed poll without touching traffic data.
	// msg is a label, never error text: API clients are served it verbatim.
	MarkNodeError(ctx context.Context, nodeKey, msg string, ts time.Time) error
	// Retention deletes buckets older than the configured windows.
	Retention(ctx context.Context, minute, hour time.Duration) error

	// NodeStatuses returns every registered node with its health snapshot,
	// ordered by (fleet, id).
	NodeStatuses(ctx context.Context, scope Scope) ([]NodeStatus, error)
	// NodeTotals returns per-entity traffic totals for one node over
	// [from, to), keyed by kind (inbound|outbound|user), sorted by
	// combined traffic descending.
	NodeTotals(ctx context.Context, nodeKey string, from, to int64) (map[string][]EntityTotal, error)
	// Series returns dense arrays aligned to From + i*Step.
	Series(ctx context.Context, p SeriesParams) ([]Series, error)
	// OverviewTraffic aggregates hub-inbound traffic (totals, per fleet,
	// top users, top nodes) plus the distinct online-user count.
	OverviewTraffic(ctx context.Context, from, to int64, scope Scope) (*OverviewTraffic, error)
	// Users lists per-user traffic over [from,to) - hub nodes only.
	// Hubs are those with traffic or online presence in [seenFrom,to).
	Users(ctx context.Context, from, to, seenFrom int64, scope Scope) ([]UserRow, error)
	// UserDetail returns totals, per-node breakdown, hubs seen in
	// [seenFrom,to), and current IP rows (including empty-IP sentinel
	// rows; callers filter for display).
	UserDetail(ctx context.Context, email string, from, to, seenFrom int64, scope Scope) (DirTotal, []EntityTotal, []UserHubSeen, []OnlineIPRow, error)
	// OnlineNow returns the current online snapshot grouped by node+email.
	OnlineNow(ctx context.Context, scope Scope) ([]OnlineRow, error)
	// Counters returns all current cumulative counters (for /metrics),
	// ordered by (node_key, name).
	Counters(ctx context.Context, scope Scope) ([]CounterRow, error)

	Close() error
}

// Node identity as registered from config/topology.
type Node struct {
	Key      string // "<fleet>/<id>"
	Fleet    string
	ID       string
	Region   string
	Type     string // hub | exit
	Hostname string
	Collect  string
}

// NodeStatus is a node row with its live health snapshot.
type NodeStatus struct {
	Node
	LastErr      string // failure label, see MarkNodeError
	LastOK       int64  // unix seconds of last successful poll, 0 = never
	LastErrTS    int64
	UptimeS      int64
	NumGoroutine int64
	AllocBytes   int64
	SysBytes     int64
	NumGC        int64
}

// Delta is one parsed counter increment.
type Delta struct {
	Kind   string // inbound | outbound | user
	Entity string // tag or email
	Dir    string // up | down
	Bytes  int64
}

// Sample is the result of one successful poll.
type Sample struct {
	NodeKey         string
	TS              time.Time
	Counters        map[string]int64 // current cumulative values (delta base for next poll)
	Deltas          []Delta
	Sys             *xray.SysStats    // nil when unavailable
	Online          []xray.OnlineUser // meaningful only when OnlineCollected
	OnlineCollected bool
}

// Aggregation modes for Series.
const (
	AggNone   = "none"   // one series per (node, entity, dir)
	AggEntity = "entity" // collapse nodes: per (entity, dir)
	AggNode   = "node"   // collapse entities: per (node, dir)
	AggTotal  = "total"  // per dir
)

// MaxSlots caps Series result width; implementations must reject wider.
const MaxSlots = 2000

type SeriesParams struct {
	Kind     string // inbound | outbound | user | online
	Node     string // node_key filter
	Type     string // hub | exit
	Entity   string
	Dir      string // up | down
	Agg      string
	Scope    Scope // permitted fleets; also carries a ?fleet= filter
	From, To int64 // unix seconds, half-open [From, To)
	Step     int64 // seconds
}

type Series struct {
	Node   string  `json:"node,omitempty"`
	Entity string  `json:"entity,omitempty"`
	Dir    string  `json:"dir,omitempty"`
	Points []int64 `json:"points"`
}

type DirTotal struct {
	Up   int64 `json:"up_bytes"`
	Down int64 `json:"down_bytes"`
}

type EntityTotal struct {
	Entity string `json:"entity"`
	DirTotal
}

type OverviewTraffic struct {
	FleetBytes  map[string]DirTotal // hub-inbound traffic per fleet
	TopUsers    []EntityTotal
	TopNodes    []EntityTotal // Entity holds the node_key
	Totals      DirTotal
	OnlineUsers int64
}

// UserHubSeen is a hub the user was active on, with the latest activity time.
type UserHubSeen struct {
	Node     string `json:"node"`
	LastSeen int64  `json:"last_seen"` // unix seconds (traffic bucket or online last_seen)
}

type UserRow struct {
	Email     string        `json:"email"`
	Hubs      []UserHubSeen `json:"hubs"`
	Up        int64         `json:"up_bytes"`
	Down      int64         `json:"down_bytes"`
	OnlineNow bool          `json:"online_now"`
}

type OnlineIPRow struct {
	Node     string `json:"node"`
	IP       string `json:"ip,omitempty"`
	LastSeen int64  `json:"last_seen"`
}

type OnlineRow struct {
	Node  string        `json:"node"`
	Email string        `json:"email"`
	IPs   []OnlineIPRow `json:"ips"`
}

type CounterRow struct {
	NodeKey string
	Name    string
	Value   int64
}
