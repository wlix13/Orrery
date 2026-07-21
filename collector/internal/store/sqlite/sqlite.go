// Package sqlite is the default embedded storage backend.
//
// Traffic deltas are written at ingest into BOTH minute and hour buckets
// (bytes += delta upserts), so there is no rollup job - retention is a
// DELETE per table and queries pick the resolution that fits the range.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/xray"
)

type Store struct {
	db *sql.DB
}

var _ store.Store = (*Store)(nil)

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?" + url.Values{
		"_pragma": []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)"},
	}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(8)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
  node_key      TEXT PRIMARY KEY,
  fleet         TEXT NOT NULL,
  id            TEXT NOT NULL,
  region        TEXT NOT NULL DEFAULT '',
  type          TEXT NOT NULL,
  hostname      TEXT NOT NULL DEFAULT '',
  collect       TEXT NOT NULL,
  last_ok       INTEGER NOT NULL DEFAULT 0,
  last_err      TEXT NOT NULL DEFAULT '',
  last_err_ts   INTEGER NOT NULL DEFAULT 0,
  uptime_s      INTEGER NOT NULL DEFAULT 0,
  num_goroutine INTEGER NOT NULL DEFAULT 0,
  alloc_bytes   INTEGER NOT NULL DEFAULT 0,
  sys_bytes     INTEGER NOT NULL DEFAULT 0,
  num_gc        INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS counters_last (
  node_key TEXT NOT NULL,
  name     TEXT NOT NULL,
  value    INTEGER NOT NULL,
  ts       INTEGER NOT NULL,
  PRIMARY KEY (node_key, name)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS traffic_minute (
  bucket   INTEGER NOT NULL,
  node_key TEXT NOT NULL,
  kind     TEXT NOT NULL,
  entity   TEXT NOT NULL,
  dir      TEXT NOT NULL,
  bytes    INTEGER NOT NULL,
  PRIMARY KEY (bucket, node_key, kind, entity, dir)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_tm_kind_entity ON traffic_minute (kind, entity, bucket);
CREATE TABLE IF NOT EXISTS traffic_hour (
  bucket   INTEGER NOT NULL,
  node_key TEXT NOT NULL,
  kind     TEXT NOT NULL,
  entity   TEXT NOT NULL,
  dir      TEXT NOT NULL,
  bytes    INTEGER NOT NULL,
  PRIMARY KEY (bucket, node_key, kind, entity, dir)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_th_kind_entity ON traffic_hour (kind, entity, bucket);
CREATE TABLE IF NOT EXISTS online_minute (
  bucket   INTEGER NOT NULL,
  node_key TEXT NOT NULL,
  count    INTEGER NOT NULL,
  PRIMARY KEY (bucket, node_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS online_hour (
  bucket   INTEGER NOT NULL,
  node_key TEXT NOT NULL,
  count    INTEGER NOT NULL,
  PRIMARY KEY (bucket, node_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS online_current (
  node_key  TEXT NOT NULL,
  email     TEXT NOT NULL,
  ip        TEXT NOT NULL,
  last_seen INTEGER NOT NULL,
  PRIMARY KEY (node_key, email, ip)
) WITHOUT ROWID;
`

// RegisterNodes reconciles the nodes table with the configured set.
func (s *Store) RegisterNodes(ctx context.Context, nodes []Node) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	keys := make([]string, 0, len(nodes))
	for _, n := range nodes {
		keys = append(keys, n.Key)

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO nodes (node_key, fleet, id, region, type, hostname, collect)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(node_key) DO UPDATE SET
			  fleet=excluded.fleet, id=excluded.id, region=excluded.region,
			  type=excluded.type, hostname=excluded.hostname, collect=excluded.collect`,
			n.Key, n.Fleet, n.ID, n.Region, n.Type, n.Hostname, n.Collect); err != nil {
			return err
		}
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")

	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}

	for _, q := range []string{
		"DELETE FROM nodes WHERE node_key NOT IN (" + placeholders + ")",
		"DELETE FROM online_current WHERE node_key NOT IN (" + placeholders + ")",
		"DELETE FROM counters_last WHERE node_key NOT IN (" + placeholders + ")",
	} {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// LastCounters loads the delta base for a node (used once per poller start).
func (s *Store) LastCounters(ctx context.Context, nodeKey string) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT name, value FROM counters_last WHERE node_key = ?", nodeKey)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	out := map[string]int64{}

	for rows.Next() {
		var name string

		var value int64
		if err := rows.Scan(&name, &value); err != nil {
			return nil, err
		}

		out[name] = value
	}

	return out, rows.Err()
}

// WriteSample persists one poll atomically.
func (s *Store) WriteSample(ctx context.Context, smp Sample) error {
	ts := smp.TS.Unix()
	minute, hour := ts-ts%60, ts-ts%3600

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := writeCounters(ctx, tx, smp, ts); err != nil {
		return err
	}

	if err := writeTrafficBuckets(ctx, tx, smp, minute, hour); err != nil {
		return err
	}

	if err := writeNodeHealth(ctx, tx, smp, ts); err != nil {
		return err
	}

	if smp.OnlineCollected {
		if err := writeOnline(ctx, tx, smp, ts, minute, hour); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func writeCounters(ctx context.Context, tx *sql.Tx, smp Sample, ts int64) error {
	upCounter, err := tx.PrepareContext(ctx, `
		INSERT INTO counters_last (node_key, name, value, ts) VALUES (?, ?, ?, ?)
		ON CONFLICT(node_key, name) DO UPDATE SET value=excluded.value, ts=excluded.ts`)
	if err != nil {
		return err
	}

	defer upCounter.Close()

	for name, value := range smp.Counters {
		if _, err := upCounter.ExecContext(ctx, smp.NodeKey, name, value, ts); err != nil {
			return err
		}
	}

	return nil
}

func writeTrafficBuckets(ctx context.Context, tx *sql.Tx, smp Sample, minute, hour int64) error {
	for table, bucket := range map[string]int64{"traffic_minute": minute, "traffic_hour": hour} {
		upTraffic, err := tx.PrepareContext(ctx, `
			INSERT INTO `+table+` (bucket, node_key, kind, entity, dir, bytes) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(bucket, node_key, kind, entity, dir) DO UPDATE SET bytes = bytes + excluded.bytes`)
		if err != nil {
			return err
		}

		for _, d := range smp.Deltas {
			if d.Bytes == 0 {
				continue
			}

			if _, err := upTraffic.ExecContext(ctx, bucket, smp.NodeKey, d.Kind, d.Entity, d.Dir, d.Bytes); err != nil {
				upTraffic.Close()
				return err
			}
		}

		upTraffic.Close()
	}

	return nil
}

func writeNodeHealth(ctx context.Context, tx *sql.Tx, smp Sample, ts int64) error {
	if smp.Sys == nil {
		_, err := tx.ExecContext(ctx,
			"UPDATE nodes SET last_ok=?, last_err='' WHERE node_key=?", ts, smp.NodeKey)

		return err
	}

	_, err := tx.ExecContext(ctx, `
		UPDATE nodes SET last_ok=?, last_err='', uptime_s=?, num_goroutine=?, alloc_bytes=?, sys_bytes=?, num_gc=?
		WHERE node_key=?`,
		ts, smp.Sys.UptimeS, smp.Sys.NumGoroutine, smp.Sys.Alloc, smp.Sys.Sys, smp.Sys.NumGC, smp.NodeKey)

	return err
}

func writeOnline(ctx context.Context, tx *sql.Tx, smp Sample, ts, minute, hour int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM online_current WHERE node_key=?", smp.NodeKey); err != nil {
		return err
	}

	for _, u := range smp.Online {
		if err := writeOnlineUser(ctx, tx, smp.NodeKey, u, ts); err != nil {
			return err
		}
	}

	count := len(smp.Online)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO online_minute (bucket, node_key, count) VALUES (?, ?, ?)
		ON CONFLICT(bucket, node_key) DO UPDATE SET count=excluded.count`,
		minute, smp.NodeKey, count); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO online_hour (bucket, node_key, count) VALUES (?, ?, ?)
		ON CONFLICT(bucket, node_key) DO UPDATE SET count=MAX(count, excluded.count)`,
		hour, smp.NodeKey, count)

	return err
}

func writeOnlineUser(ctx context.Context, tx *sql.Tx, nodeKey string, u xray.OnlineUser, ts int64) error {
	if len(u.IPs) == 0 {
		// Older Xray without IP-list RPCs: keep presence with a sentinel row.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO online_current (node_key, email, ip, last_seen) VALUES (?, ?, '', ?)
			ON CONFLICT DO NOTHING`, nodeKey, u.Email, ts)

		return err
	}

	for _, ip := range u.IPs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO online_current (node_key, email, ip, last_seen) VALUES (?, ?, ?, ?)
			ON CONFLICT(node_key, email, ip) DO UPDATE SET last_seen=excluded.last_seen`,
			nodeKey, u.Email, ip.IP, ip.LastSeen); err != nil {
			return err
		}
	}

	return nil
}

// MarkNodeError records a failed poll without touching traffic data.
func (s *Store) MarkNodeError(ctx context.Context, nodeKey, msg string, ts time.Time) error {
	if len(msg) > 500 {
		msg = msg[:500]
	}

	_, err := s.db.ExecContext(ctx,
		"UPDATE nodes SET last_err=?, last_err_ts=? WHERE node_key=?", msg, ts.Unix(), nodeKey)

	return err
}

// Retention deletes buckets older than the configured windows.
func (s *Store) Retention(ctx context.Context, minute, hour time.Duration) error {
	now := time.Now().Unix()
	for table, cutoff := range map[string]int64{
		"traffic_minute": now - int64(minute.Seconds()),
		"online_minute":  now - int64(minute.Seconds()),
		"traffic_hour":   now - int64(hour.Seconds()),
		"online_hour":    now - int64(hour.Seconds()),
	} {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE bucket < ?", cutoff); err != nil {
			return fmt.Errorf("retention %s: %w", table, err)
		}
	}

	return nil
}
