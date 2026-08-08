package sqlite_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/store/sqlite"
	"github.com/wlix13/orrery/collector/internal/store/storetest"
	"github.com/wlix13/orrery/collector/internal/xray"
)

func TestConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store {
		t.Helper()

		s, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { s.Close() })

		return s
	})
}

// A fleet's pollers and the retention sweep all write the same file. SQLite
// takes one writer, so they must queue rather than fail, and every sample
// must survive.
func TestConcurrentWritesAndSweep(t *testing.T) {
	s, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { s.Close() })

	ctx := context.Background()

	const (
		nodes    = 12
		polls    = 15
		entities = 25
	)

	reg := make([]store.Node, 0, nodes)
	for i := range nodes {
		id := fmt.Sprintf("hub%02d", i)
		reg = append(reg, store.Node{
			Key: "main/" + id, Fleet: "main", ID: id, Type: "hub", Collect: "full",
		})
	}

	if err := s.RegisterNodes(ctx, reg); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Truncate(time.Hour).Add(-time.Hour)
	stale := base.Add(-100 * 24 * time.Hour) // older than both retention windows below
	errs := make(chan error, nodes+1)

	var wg sync.WaitGroup

	for _, n := range reg {
		wg.Go(func() { writePollsAndStale(ctx, s, n.Key, base, stale, polls, entities, errs) })
	}

	// Contends for the same write lock throughout.
	wg.Go(func() { runRetentionSweep(ctx, s, polls, errs) })

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	assertStaleSwept(t, s, stale)
	assertNothingDropped(t, s, base, nodes, polls, entities)
}

func writePollsAndStale(ctx context.Context, s store.Store, key string, base, stale time.Time, polls, entities int, errs chan<- error) {
	for p := range polls {
		if err := s.WriteSample(ctx, sample(key, base.Add(time.Duration(p)*time.Minute), entities)); err != nil {
			errs <- fmt.Errorf("%s poll %d: %w", key, p, err)
			return
		}
	}

	if err := s.WriteSample(ctx, sample(key, stale, entities)); err != nil {
		errs <- fmt.Errorf("%s stale: %w", key, err)
	}
}

func runRetentionSweep(ctx context.Context, s store.Store, polls int, errs chan<- error) {
	for range polls {
		if err := s.Retention(ctx, 72*time.Hour, 90*24*time.Hour); err != nil {
			errs <- fmt.Errorf("retention: %w", err)
			return
		}
	}
}

// Swept from both bucket tables, however many chunks it took.
func assertStaleSwept(t *testing.T, s store.Store, stale time.Time) {
	t.Helper()

	ctx := context.Background()

	for _, step := range []int64{60, 3600} {
		staleSeries, err := s.Series(ctx, store.SeriesParams{
			From: stale.Add(-time.Minute).Unix(), To: stale.Add(time.Minute).Unix(), Step: step,
			Kind: "user", Agg: store.AggTotal, Scope: store.AllFleets(),
		})
		if err != nil {
			t.Fatal(err)
		}

		for _, sr := range staleSeries {
			for slot, got := range sr.Points {
				if got != 0 {
					t.Fatalf("stale slot %d (step %d) = %d bytes, want 0", slot, step, got)
				}
			}
		}
	}
}

// Nothing was dropped: every node wrote the same bytes into every minute.
func assertNothingDropped(t *testing.T, s store.Store, base time.Time, nodes, polls, entities int) {
	t.Helper()

	series, err := s.Series(context.Background(), store.SeriesParams{
		From: base.Unix(), To: base.Add(time.Duration(polls) * time.Minute).Unix(), Step: 60,
		Kind: "user", Agg: store.AggTotal, Scope: store.AllFleets(),
	})
	if err != nil {
		t.Fatal(err)
	}

	want := int64(nodes * entities * 10)

	for _, sr := range series {
		for slot, got := range sr.Points {
			if got != want {
				t.Fatalf("slot %d = %d bytes, want %d", slot, got, want)
			}
		}
	}
}

func sample(nodeKey string, ts time.Time, entities int) store.Sample {
	smp := store.Sample{
		NodeKey:         nodeKey,
		TS:              ts,
		Counters:        make(map[string]int64, entities),
		OnlineCollected: true,
	}

	for e := range entities {
		email := fmt.Sprintf("u%02d@ns", e)
		smp.Counters["user>>>"+email+">>>traffic>>>downlink"] = int64(e)
		smp.Deltas = append(smp.Deltas, store.Delta{Kind: "user", Entity: email, Dir: "down", Bytes: 10})
		smp.Online = append(smp.Online, xray.OnlineUser{Email: email})
	}

	return smp
}
