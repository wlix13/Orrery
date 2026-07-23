// Package poller runs one polling loop per node: query counters over
// gRPC, diff against the previous cumulative values, persist the deltas.
package poller

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wlix13/orrery/collector/internal/config"
	"github.com/wlix13/orrery/collector/internal/store"
	"github.com/wlix13/orrery/collector/internal/xray"
)

// Target is one resolved, pollable node.
type Target struct {
	Node    store.Node
	Addr    string        // Xray API endpoint as seen by the dialer
	Dial    xray.DialFunc // direct TCP or SSH tunnel
	Collect string        // full | traffic (off never reaches the poller)
}

type Poller struct {
	store store.Store
	log   *slog.Logger

	client       *xray.Client
	last         map[string]int64 // cumulative counter values from previous poll
	warnedShapes map[string]bool  // unknown counter shapes, logged once
	target       Target
	interval     time.Duration
	timeout      time.Duration
}

func New(t Target, st store.Store, cfg config.PollConfig, log *slog.Logger) *Poller {
	return &Poller{
		target:       t,
		store:        st,
		interval:     cfg.Interval.D(),
		timeout:      cfg.Timeout.D(),
		log:          log.With("node", t.Node.Key),
		warnedShapes: map[string]bool{},
	}
}

// Run polls until ctx is cancelled. Start is jittered so a fleet of
// pollers doesn't stampede the bastion's network every interval.
func (p *Poller) Run(ctx context.Context) {
	defer func() {
		if p.client != nil {
			p.client.Close()
		}
	}()

	jitter := time.Duration(rand.Int64N(int64(p.interval)))
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		p.poll(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	if p.last == nil {
		last, err := p.store.LastCounters(ctx, p.target.Node.Key)
		if err != nil {
			p.log.Error("load delta base", "err", err)
			return
		}

		p.last = last
	}

	if p.client == nil {
		client, err := xray.New(p.target.Addr, p.target.Dial)
		if err != nil {
			p.fail(ctx, err, "poll failed")
			return
		}

		p.client = client
	}

	stats, err := p.client.QueryAll(ctx)
	if err != nil {
		// Drop the client so the next poll rebuilds the connection (and,
		// for SSH, lets the dialer re-establish the tunnel).
		p.client.Close()
		p.client = nil
		p.fail(ctx, err, pollReason(err))

		return
	}

	smp := buildSample(p.target.Node.Key, time.Now(), stats, p.last, func(name string) {
		if !p.warnedShapes[name] {
			p.warnedShapes[name] = true
			p.log.Warn("skipping unrecognized counter", "name", name)
		}
	})

	if sys, err := p.client.SysStats(ctx); err != nil {
		p.log.Warn("sys stats", "err", err)
	} else {
		smp.Sys = sys
	}

	if p.target.Collect == config.CollectFull {
		online, supported, err := p.client.OnlineUsers(ctx)

		switch {
		case err != nil:
			p.log.Warn("online users", "err", err)
		case supported:
			smp.Online = online
			smp.OnlineCollected = true
		}
	}

	if err := p.store.WriteSample(ctx, smp); err != nil {
		p.fail(ctx, err, "storage write failed")
		return
	}

	p.last = smp.Counters
}

// pollReason labels a failed RPC; the error itself quotes the node's address.
func pollReason(err error) string {
	switch status.Code(err) {
	case codes.Unavailable:
		return "node unreachable"
	case codes.DeadlineExceeded:
		return "xray api timed out"
	case codes.Unauthenticated, codes.PermissionDenied:
		return "xray api refused the request"
	case codes.Unimplemented:
		return "xray api method unsupported"
	default:
		return "poll failed"
	}
}

// fail logs the error and stores only the label, which the API serves.
func (p *Poller) fail(ctx context.Context, err error, reason string) {
	p.log.Warn("poll failed", "reason", reason, "err", err)
	// Best-effort: the parent ctx may already be cancelled on shutdown.
	if mErr := p.store.MarkNodeError(context.WithoutCancel(ctx), p.target.Node.Key, reason, time.Now()); mErr != nil {
		p.log.Error("record poll error", "err", mErr)
	}
}

// buildSample turns raw cumulative counters into a Sample: current values
// (the next poll's delta base) plus per-counter increments. A value lower
// than the previous one means Xray restarted - the whole current value is
// the increment then. Unrecognized counter shapes are reported and skipped.
func buildSample(nodeKey string, ts time.Time, stats []xray.Stat, last map[string]int64, warnUnknown func(name string)) store.Sample {
	smp := store.Sample{
		NodeKey:  nodeKey,
		TS:       ts,
		Counters: make(map[string]int64, len(stats)),
	}

	for _, st := range stats {
		kind, entity, dir, ok := xray.ParseCounterName(st.Name)
		if !ok {
			warnUnknown(st.Name)
			continue
		}

		smp.Counters[st.Name] = st.Value

		delta := st.Value - last[st.Name]
		if delta < 0 {
			delta = st.Value
		}

		if delta > 0 {
			smp.Deltas = append(smp.Deltas, store.Delta{Kind: kind, Entity: entity, Dir: dir, Bytes: delta})
		}
	}

	return smp
}

// DirectDial returns a DialFunc for plain TCP.
func DirectDial() xray.DialFunc {
	d := &net.Dialer{Timeout: 10 * time.Second}

	return func(ctx context.Context, addr string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp", addr)
	}
}
