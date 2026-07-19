package sqlite

import "github.com/wlix13/orrery/collector/internal/store"

// Aliases keep the implementation readable - the canonical definitions
// live in the parent store package.
type (
	Node            = store.Node
	NodeStatus      = store.NodeStatus
	Delta           = store.Delta
	Sample          = store.Sample
	SeriesParams    = store.SeriesParams
	Series          = store.Series
	DirTotal        = store.DirTotal
	EntityTotal     = store.EntityTotal
	OverviewTraffic = store.OverviewTraffic
	UserRow         = store.UserRow
	UserHubSeen     = store.UserHubSeen
	OnlineIPRow     = store.OnlineIPRow
	OnlineRow       = store.OnlineRow
	CounterRow      = store.CounterRow
	Scope           = store.Scope
)

const (
	AggNone   = store.AggNone
	AggEntity = store.AggEntity
	AggNode   = store.AggNode
	AggTotal  = store.AggTotal
	maxSlots  = store.MaxSlots
)
