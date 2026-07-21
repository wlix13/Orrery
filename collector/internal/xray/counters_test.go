package xray

import "testing"

func TestParseCounterName(t *testing.T) {
	tests := []struct {
		name              string
		kind, entity, dir string
		ok                bool
	}{
		{"inbound>>>direct-xhttp>>>traffic>>>uplink", "inbound", "direct-xhttp", "up", true},
		{"inbound>>>cdn-xhttp>>>traffic>>>downlink", "inbound", "cdn-xhttp", "down", true},
		{"outbound>>>nlA00>>>traffic>>>uplink", "outbound", "nlA00", "up", true},
		{"user>>>alice@main.conglomerate>>>traffic>>>downlink", "user", "alice@main.conglomerate", "down", true},
		{"user>>>mskA00-nlA00@ns>>>traffic>>>uplink", "user", "mskA00-nlA00@ns", "up", true},
		// Rejected shapes.
		{"user>>>alice@ns>>>online", "", "", "", false},
		{"inbound>>>tag>>>traffic>>>sideways", "", "", "", false},
		{"weird>>>tag>>>traffic>>>uplink", "", "", "", false},
		{"inbound>>>>>>traffic>>>uplink", "", "", "", false},
		{"", "", "", "", false},
		{"inbound>>>tag>>>bytes>>>uplink", "", "", "", false},
	}
	for _, tt := range tests {
		kind, entity, dir, ok := ParseCounterName(tt.name)
		if ok != tt.ok || kind != tt.kind || entity != tt.entity || dir != tt.dir {
			t.Errorf("ParseCounterName(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tt.name, kind, entity, dir, ok, tt.kind, tt.entity, tt.dir, tt.ok)
		}
	}
}
