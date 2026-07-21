// Package mongo is the optional MongoDB storage backend, selected by a
// mongodb:// URI. It is a document-model mirror of the sqlite package:
// same collections-as-tables layout, same bucket math, same query
// semantics - see sqlite's package doc for the shared design rationale.
//
// MongoDB standalone deployments (the expected topology here) do not
// support multi-document transactions, so writes that touch several
// collections are done as an ORDERED sequence of best-effort operations
// rather than one atomic transaction. Each write step documents the
// crash-consistency trade-off it accepts.
package mongo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/wlix13/orrery/collector/internal/store"
)

// Store is the MongoDB-backed implementation of store.Store.
type Store struct {
	client *mongo.Client
	db     *mongo.Database

	nodes         *mongo.Collection
	countersLast  *mongo.Collection
	trafficMinute *mongo.Collection
	trafficHour   *mongo.Collection
	onlineMinute  *mongo.Collection
	onlineHour    *mongo.Collection
	onlineCurrent *mongo.Collection
}

var _ store.Store = (*Store)(nil)

// Open connects to a MongoDB server, verifies reachability with a bounded
// ping, and ensures all collection indexes exist. The database name is
// taken from the URI's path component (e.g. "mongodb://host/orrery"),
// defaulting to "orrery" when the URI carries none.
func Open(ctx context.Context, uri string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ping: %w", err)
	}

	db := client.Database(dbNameFromURI(uri))

	s := &Store{
		client:        client,
		db:            db,
		nodes:         db.Collection("nodes"),
		countersLast:  db.Collection("counters_last"),
		trafficMinute: db.Collection("traffic_minute"),
		trafficHour:   db.Collection("traffic_hour"),
		onlineMinute:  db.Collection("online_minute"),
		onlineHour:    db.Collection("online_hour"),
		onlineCurrent: db.Collection("online_current"),
	}

	if err := s.ensureIndexes(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("ensure indexes: %w", err)
	}

	return s, nil
}

func dbNameFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return "orrery"
	}

	name := strings.Trim(u.Path, "/")
	if name == "" {
		return "orrery"
	}

	return name
}

// Close disconnects the underlying client.
func (s *Store) Close() error {
	return s.client.Disconnect(context.Background())
}

// DropDatabase drops the backing database. It is an administrative helper
// for tests (fresh-store-per-run cleanup); it is not part of the
// store.Store contract.
func (s *Store) DropDatabase(ctx context.Context) error {
	return s.db.Drop(ctx)
}

// ensureIndexes creates every index used by the query layer. CreateMany is
// idempotent: re-running it against an already-indexed collection with the
// same key/option spec is a no-op.
func (s *Store) ensureIndexes(ctx context.Context) error {
	unique := options.Index().SetUnique(true)

	specs := []struct {
		coll   *mongo.Collection
		models []mongo.IndexModel
	}{
		{s.countersLast, []mongo.IndexModel{
			{Keys: bson.D{{Key: "node_key", Value: 1}, {Key: "name", Value: 1}}, Options: unique},
		}},
		{s.trafficMinute, trafficIndexes()},
		{s.trafficHour, trafficIndexes()},
		{s.onlineMinute, []mongo.IndexModel{
			{Keys: bson.D{{Key: "bucket", Value: 1}, {Key: "node_key", Value: 1}}, Options: unique},
		}},
		{s.onlineHour, []mongo.IndexModel{
			{Keys: bson.D{{Key: "bucket", Value: 1}, {Key: "node_key", Value: 1}}, Options: unique},
		}},
		{s.onlineCurrent, []mongo.IndexModel{
			{
				Keys:    bson.D{{Key: "node_key", Value: 1}, {Key: "email", Value: 1}, {Key: "ip", Value: 1}},
				Options: unique,
			},
		}},
	}

	for _, spec := range specs {
		if _, err := spec.coll.Indexes().CreateMany(ctx, spec.models); err != nil {
			return fmt.Errorf("create indexes on %s: %w", spec.coll.Name(), err)
		}
	}

	return nil
}

func trafficIndexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "bucket", Value: 1}, {Key: "node_key", Value: 1},
				{Key: "kind", Value: 1}, {Key: "entity", Value: 1}, {Key: "dir", Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "kind", Value: 1}, {Key: "entity", Value: 1}, {Key: "bucket", Value: 1}}},
	}
}

// nodeDoc mirrors the "nodes" table: _id is the node key.
type nodeDoc struct {
	Key          string `bson:"_id"`
	Fleet        string `bson:"fleet"`
	ID           string `bson:"id"`
	Region       string `bson:"region"`
	Type         string `bson:"type"`
	Hostname     string `bson:"hostname"`
	Collect      string `bson:"collect"`
	LastErr      string `bson:"last_err"`
	LastOK       int64  `bson:"last_ok"`
	LastErrTS    int64  `bson:"last_err_ts"`
	UptimeS      int64  `bson:"uptime_s"`
	NumGoroutine int64  `bson:"num_goroutine"`
	AllocBytes   int64  `bson:"alloc_bytes"`
	SysBytes     int64  `bson:"sys_bytes"`
	NumGC        int64  `bson:"num_gc"`
}

func (d nodeDoc) node() store.Node {
	return store.Node{
		Key: d.Key, Fleet: d.Fleet, ID: d.ID, Region: d.Region,
		Type: d.Type, Hostname: d.Hostname, Collect: d.Collect,
	}
}

// counterDoc mirrors "counters_last".
type counterDoc struct {
	NodeKey string `bson:"node_key"`
	Name    string `bson:"name"`
	Value   int64  `bson:"value"`
	TS      int64  `bson:"ts"`
}

// onlineCurrentDoc mirrors "online_current".
type onlineCurrentDoc struct {
	NodeKey  string `bson:"node_key"`
	Email    string `bson:"email"`
	IP       string `bson:"ip"`
	LastSeen int64  `bson:"last_seen"`
}

// RegisterNodes reconciles the nodes collection with the configured set.
// Upserts run first so newly configured nodes exist before pruning removes
// rows for anything no longer in the set (nodes, online_current,
// counters_last - traffic/online buckets are left in place but become
// unreachable since every query scopes to registered nodes).
func (s *Store) RegisterNodes(ctx context.Context, nodes []store.Node) error {
	keys := make([]string, 0, len(nodes))
	models := make([]mongo.WriteModel, 0, len(nodes))

	for _, n := range nodes {
		keys = append(keys, n.Key)

		set := bson.D{
			{Key: "fleet", Value: n.Fleet}, {Key: "id", Value: n.ID}, {Key: "region", Value: n.Region},
			{Key: "type", Value: n.Type}, {Key: "hostname", Value: n.Hostname}, {Key: "collect", Value: n.Collect},
		}
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.D{{Key: "_id", Value: n.Key}}).
			SetUpdate(bson.D{{Key: "$set", Value: set}}).
			SetUpsert(true))
	}

	if len(models) > 0 {
		if _, err := s.nodes.BulkWrite(ctx, models); err != nil {
			return fmt.Errorf("upsert nodes: %w", err)
		}
	}

	notIn := bson.D{{Key: "$nin", Value: keys}}

	if _, err := s.nodes.DeleteMany(ctx, bson.D{{Key: "_id", Value: notIn}}); err != nil {
		return fmt.Errorf("prune nodes: %w", err)
	}

	if _, err := s.onlineCurrent.DeleteMany(ctx, bson.D{{Key: "node_key", Value: notIn}}); err != nil {
		return fmt.Errorf("prune online_current: %w", err)
	}

	if _, err := s.countersLast.DeleteMany(ctx, bson.D{{Key: "node_key", Value: notIn}}); err != nil {
		return fmt.Errorf("prune counters_last: %w", err)
	}

	return nil
}

// LastCounters loads the delta base for a node (used once per poller start).
func (s *Store) LastCounters(ctx context.Context, nodeKey string) (map[string]int64, error) {
	cur, err := s.countersLast.Find(ctx, bson.D{{Key: "node_key", Value: nodeKey}},
		options.Find().SetProjection(bson.D{{Key: "name", Value: 1}, {Key: "value", Value: 1}}))
	if err != nil {
		return nil, err
	}

	defer func() { _ = cur.Close(ctx) }()

	out := map[string]int64{}

	for cur.Next(ctx) {
		var doc struct {
			Name  string `bson:"name"`
			Value int64  `bson:"value"`
		}

		if err := cur.Decode(&doc); err != nil {
			return nil, err
		}

		out[doc.Name] = doc.Value
	}

	return out, cur.Err()
}

// MarkNodeError records a failed poll without touching traffic data. A
// missing node is a silent no-op, matching sqlite's UPDATE semantics.
func (s *Store) MarkNodeError(ctx context.Context, nodeKey, msg string, ts time.Time) error {
	if len(msg) > 500 {
		msg = msg[:500]
	}

	_, err := s.nodes.UpdateOne(ctx, bson.D{{Key: "_id", Value: nodeKey}}, bson.D{{Key: "$set", Value: bson.D{
		{Key: "last_err", Value: msg}, {Key: "last_err_ts", Value: ts.Unix()},
	}}})

	return err
}

// Retention deletes buckets older than the configured windows.
func (s *Store) Retention(ctx context.Context, minute, hour time.Duration) error {
	now := time.Now().Unix()

	windows := []struct {
		coll   *mongo.Collection
		cutoff int64
	}{
		{s.trafficMinute, now - int64(minute.Seconds())},
		{s.onlineMinute, now - int64(minute.Seconds())},
		{s.trafficHour, now - int64(hour.Seconds())},
		{s.onlineHour, now - int64(hour.Seconds())},
	}

	for _, w := range windows {
		filter := bson.D{{Key: "bucket", Value: bson.D{{Key: "$lt", Value: w.cutoff}}}}
		if _, err := w.coll.DeleteMany(ctx, filter); err != nil {
			return fmt.Errorf("retention %s: %w", w.coll.Name(), err)
		}
	}

	return nil
}
