package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/xray"
)

// WriteSample persists one poll. MongoDB standalone has no multi-document
// transactions, so the steps below run as an ORDERED best-effort sequence
// instead of one atomic write:
//
//  1. counters_last upserts, first. If the process crashes right after this
//     step, the next poll's delta base already reflects this sample, so a
//     crash can only under-count the traffic delta for the interval that
//     was in flight - never double-count it on the following poll.
//  2. traffic buckets ($inc into both the minute and hour collections).
//  3. node health ($set last_ok/last_err, plus sys fields when present).
//  4. the online snapshot, only when this poll actually collected it.
//
// A crash between steps loses at most the later steps for this one poll;
// nothing already committed is ever rolled back or double-applied.
func (s *Store) WriteSample(ctx context.Context, smp store.Sample) error {
	ts := smp.TS.Unix()
	minute, hour := ts-ts%60, ts-ts%3600

	if err := s.writeCounters(ctx, smp, ts); err != nil {
		return err
	}

	if err := s.writeTrafficBuckets(ctx, smp, minute, hour); err != nil {
		return err
	}

	if err := s.writeNodeHealth(ctx, smp, ts); err != nil {
		return err
	}

	if smp.OnlineCollected {
		if err := s.writeOnline(ctx, smp, ts, minute, hour); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) writeCounters(ctx context.Context, smp store.Sample, ts int64) error {
	if len(smp.Counters) == 0 {
		return nil
	}

	models := make([]mongo.WriteModel, 0, len(smp.Counters))

	for name, value := range smp.Counters {
		filter := bson.D{{Key: "node_key", Value: smp.NodeKey}, {Key: "name", Value: name}}
		update := bson.D{{Key: "$set", Value: bson.D{{Key: "value", Value: value}, {Key: "ts", Value: ts}}}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	_, err := s.countersLast.BulkWrite(ctx, models)

	return err
}

func (s *Store) writeTrafficBuckets(ctx context.Context, smp store.Sample, minute, hour int64) error {
	buckets := []struct {
		coll   *mongo.Collection
		bucket int64
	}{
		{s.trafficMinute, minute},
		{s.trafficHour, hour},
	}

	for _, b := range buckets {
		if err := s.writeTrafficBucket(ctx, b.coll, smp, b.bucket); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) writeTrafficBucket(ctx context.Context, coll *mongo.Collection, smp store.Sample, bucket int64) error {
	models := make([]mongo.WriteModel, 0, len(smp.Deltas))

	for _, d := range smp.Deltas {
		if d.Bytes == 0 {
			continue
		}

		filter := bson.D{
			{Key: "bucket", Value: bucket}, {Key: "node_key", Value: smp.NodeKey},
			{Key: "kind", Value: d.Kind}, {Key: "entity", Value: d.Entity}, {Key: "dir", Value: d.Dir},
		}
		update := bson.D{{Key: "$inc", Value: bson.D{{Key: "bytes", Value: d.Bytes}}}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	if len(models) == 0 {
		return nil
	}

	_, err := coll.BulkWrite(ctx, models)

	return err
}

func (s *Store) writeNodeHealth(ctx context.Context, smp store.Sample, ts int64) error {
	set := bson.D{{Key: "last_ok", Value: ts}, {Key: "last_err", Value: ""}}

	if smp.Sys != nil {
		set = append(set,
			bson.E{Key: "uptime_s", Value: int64(smp.Sys.UptimeS)},
			bson.E{Key: "num_goroutine", Value: int64(smp.Sys.NumGoroutine)},
			bson.E{Key: "alloc_bytes", Value: int64(smp.Sys.Alloc)},
			bson.E{Key: "sys_bytes", Value: int64(smp.Sys.Sys)},
			bson.E{Key: "num_gc", Value: int64(smp.Sys.NumGC)},
		)
	}

	_, err := s.nodes.UpdateOne(ctx, bson.D{{Key: "_id", Value: smp.NodeKey}}, bson.D{{Key: "$set", Value: set}})

	return err
}

// writeOnline replaces the node's online snapshot and updates the online
// gauges. The prior rows are deleted before the fresh ones are inserted, so
// a crash mid-step can leave the node's presence rows empty for one poll
// but never stale (no user lingers as "online" past their last sighting).
func (s *Store) writeOnline(ctx context.Context, smp store.Sample, ts, minute, hour int64) error {
	if _, err := s.onlineCurrent.DeleteMany(ctx, bson.D{{Key: "node_key", Value: smp.NodeKey}}); err != nil {
		return err
	}

	if err := s.writeOnlineUsers(ctx, smp, ts); err != nil {
		return err
	}

	count := int64(len(smp.Online))

	minuteFilter := bson.D{{Key: "bucket", Value: minute}, {Key: "node_key", Value: smp.NodeKey}}
	minuteUpdate := bson.D{{Key: "$set", Value: bson.D{{Key: "count", Value: count}}}}

	if _, err := s.onlineMinute.UpdateOne(ctx, minuteFilter, minuteUpdate, options.UpdateOne().SetUpsert(true)); err != nil {
		return err
	}

	hourFilter := bson.D{{Key: "bucket", Value: hour}, {Key: "node_key", Value: smp.NodeKey}}
	hourUpdate := bson.D{{Key: "$max", Value: bson.D{{Key: "count", Value: count}}}}
	_, err := s.onlineHour.UpdateOne(ctx, hourFilter, hourUpdate, options.UpdateOne().SetUpsert(true))

	return err
}

// writeOnlineUsers inserts the current snapshot's presence rows. Every
// write is an upsert keyed by (node_key, email, ip) even though the rows
// for this node were just deleted, so a duplicate report within the same
// poll (e.g. the same empty-IP user twice) can't collide - the second
// write is a same-value no-op, mirroring sqlite's ON CONFLICT DO NOTHING /
// DO UPDATE pairing.
func (s *Store) writeOnlineUsers(ctx context.Context, smp store.Sample, ts int64) error {
	if len(smp.Online) == 0 {
		return nil
	}

	var models []mongo.WriteModel

	for _, u := range smp.Online {
		models = append(models, onlineUserModels(smp.NodeKey, u, ts)...)
	}

	if len(models) == 0 {
		return nil
	}

	_, err := s.onlineCurrent.BulkWrite(ctx, models)

	return err
}

func onlineUserModels(nodeKey string, u xray.OnlineUser, ts int64) []mongo.WriteModel {
	if len(u.IPs) == 0 {
		// Older Xray without IP-list RPCs: keep presence with a sentinel
		// row (empty ip). $setOnInsert means a matching row (only possible
		// from a duplicate within this same batch) is left untouched.
		filter := bson.D{{Key: "node_key", Value: nodeKey}, {Key: "email", Value: u.Email}, {Key: "ip", Value: ""}}
		update := bson.D{{Key: "$setOnInsert", Value: bson.D{{Key: "last_seen", Value: ts}}}}

		return []mongo.WriteModel{mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true)}
	}

	models := make([]mongo.WriteModel, 0, len(u.IPs))

	for _, ip := range u.IPs {
		filter := bson.D{{Key: "node_key", Value: nodeKey}, {Key: "email", Value: u.Email}, {Key: "ip", Value: ip.IP}}
		update := bson.D{{Key: "$set", Value: bson.D{{Key: "last_seen", Value: ip.LastSeen}}}}
		models = append(models, mongo.NewUpdateOneModel().SetFilter(filter).SetUpdate(update).SetUpsert(true))
	}

	return models
}
