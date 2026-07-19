package xray

import "strings"

// Traffic directions as stored by Orrery (Xray uses uplink/downlink).
const (
	DirUp   = "up"
	DirDown = "down"
)

// Counter kinds.
const (
	KindInbound  = "inbound"
	KindOutbound = "outbound"
	KindUser     = "user"
)

// ParseCounterName splits an Xray stat counter name of the form
// "kind>>>entity>>>traffic>>>direction" (e.g.
// "inbound>>>direct-xhttp>>>traffic>>>uplink",
// "user>>>alice@ns>>>traffic>>>downlink").
// ok is false for any other shape (e.g. online maps never appear here).
func ParseCounterName(name string) (kind, entity, dir string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[2] != "traffic" {
		return "", "", "", false
	}

	switch parts[0] {
	case KindInbound, KindOutbound, KindUser:
		kind = parts[0]
	default:
		return "", "", "", false
	}

	switch parts[3] {
	case "uplink":
		dir = DirUp
	case "downlink":
		dir = DirDown
	default:
		return "", "", "", false
	}

	if parts[1] == "" {
		return "", "", "", false
	}

	return kind, parts[1], dir, true
}
