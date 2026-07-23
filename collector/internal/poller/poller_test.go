package poller

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/wlix13/orrery/collector/internal/xray"
)

func TestPollReasonHidesAddresses(t *testing.T) {
	// gRPC inlines the whole dial chain, node address included, in its message.
	unavailable := fmt.Errorf("QueryStats: %w", status.Error(codes.Unavailable,
		"connection error: desc = transport: Error while dialing ssh dial 203.0.113.7:22: connect: connection refused"))

	tests := map[string]struct {
		err  error
		want string
	}{
		"dial failure":  {unavailable, "node unreachable"},
		"rpc deadline":  {fmt.Errorf("QueryStats: %w", status.Error(codes.DeadlineExceeded, "ctx")), "xray api timed out"},
		"rpc denied":    {fmt.Errorf("QueryStats: %w", status.Error(codes.PermissionDenied, "denied")), "xray api refused the request"},
		"rpc no method": {fmt.Errorf("QueryStats: %w", status.Error(codes.Unimplemented, "nope")), "xray api method unsupported"},
		"plain error":   {errors.New("grpc client for 203.0.113.7:10085: bad target"), "poll failed"},
	}

	for name, tc := range tests {
		got := pollReason(tc.err)
		if got != tc.want {
			t.Errorf("%s: pollReason = %q, want %q", name, got, tc.want)
		}

		if strings.Contains(got, "203.0.113.7") {
			t.Errorf("%s: reason %q leaks the node address", name, got)
		}
	}
}

func TestBuildSampleDeltas(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0)
	last := map[string]int64{
		"inbound>>>direct-xhttp>>>traffic>>>uplink":   1000,
		"inbound>>>direct-xhttp>>>traffic>>>downlink": 5000,
		"user>>>alice@ns>>>traffic>>>uplink":          300,
	}
	stats := []xray.Stat{
		{Name: "inbound>>>direct-xhttp>>>traffic>>>uplink", Value: 1500},   // normal growth
		{Name: "inbound>>>direct-xhttp>>>traffic>>>downlink", Value: 5000}, // unchanged
		{Name: "user>>>alice@ns>>>traffic>>>uplink", Value: 120},           // xray restarted
		{Name: "outbound>>>nlA00>>>traffic>>>uplink", Value: 42},           // brand new counter
		{Name: "some>>>garbage", Value: 7},                                 // unknown shape
	}

	var warned []string

	smp := buildSample("main/mskA00", ts, stats, last, func(n string) { warned = append(warned, n) })

	if smp.NodeKey != "main/mskA00" || !smp.TS.Equal(ts) {
		t.Fatalf("identity fields wrong: %+v", smp)
	}

	if len(warned) != 1 || warned[0] != "some>>>garbage" {
		t.Errorf("warned = %v, want [some>>>garbage]", warned)
	}
	// Unknown counters must not become the next delta base.
	if _, ok := smp.Counters["some>>>garbage"]; ok {
		t.Error("garbage counter leaked into Counters")
	}

	if len(smp.Counters) != 4 {
		t.Errorf("Counters len = %d, want 4", len(smp.Counters))
	}

	want := map[string]int64{ // key: entity+dir
		"direct-xhttp/up": 500, // 1500-1000
		"alice@ns/up":     120, // reset → full current value
		"nlA00/up":        42,  // new counter → full current value
	}

	got := map[string]int64{}
	for _, d := range smp.Deltas {
		got[d.Entity+"/"+d.Dir] = d.Bytes
	}

	if len(got) != len(want) {
		t.Fatalf("deltas = %+v, want %+v", got, want)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("delta[%s] = %d, want %d", k, got[k], v)
		}
	}
}
