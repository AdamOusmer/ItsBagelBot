package ui

import "fmt"

// RPCEndpoint is the rendered health of one fleet request-reply subject. RPC is
// core NATS (not JetStream), so the only broker-visible signal is whether a
// responder is currently subscribed and how many messages it has handled; there
// is no per-call success/error count. Populated from the NATS monitor (see
// internal/web/rpcmon.go). Lives in ui so the templ components can share it.
type RPCEndpoint struct {
	Name       string // friendly label, e.g. "User administration"
	Subject    string // the request subject tested for a responder
	Section    string // grouping bucket: "Operator", "Projections", "Internal"
	Responders int    // number of subscribed responders (queue-group members)
	Queue      string // queue group, when the responder uses one
	Msgs       uint64 // messages the responder(s) have delivered (lifetime)
	Reachable  bool   // true when at least one responder is subscribed
}

// StatusText summarises responder presence: the operator's "no answered one".
func (e RPCEndpoint) StatusText() string {
	if !e.Reachable {
		return "NO RESPONDER"
	}
	if e.Responders == 1 {
		return "1 responder"
	}
	return fmt.Sprintf("%d responders", e.Responders)
}

// Tone drives the row colour: a subject with no responder is a hard failure
// (requests to it return no-responders), everything else is healthy.
func (e RPCEndpoint) Tone() string {
	if !e.Reachable {
		return "rpc-row down"
	}
	return "rpc-row ok"
}

// MsgsText renders the lifetime delivered count, or a dash when unreachable.
func (e RPCEndpoint) MsgsText() string {
	if !e.Reachable {
		return "—"
	}
	return fmt.Sprint(e.Msgs)
}

// QueueText renders the queue group, or a dash for non-queue responders.
func (e RPCEndpoint) QueueText() string {
	if e.Queue == "" {
		return "—"
	}
	return e.Queue
}

// RPCSection is a titled group of RPC endpoints for the UI.
type RPCSection struct {
	Title     string
	Endpoints []RPCEndpoint
}

// RPCSections groups endpoints by their Section in a fixed, meaningful order.
func RPCSections(eps []RPCEndpoint) []RPCSection {
	order := []string{"Operator", "Projections", "Internal"}
	var out []RPCSection
	for _, title := range order {
		var group []RPCEndpoint
		for _, e := range eps {
			if e.Section == title {
				group = append(group, e)
			}
		}
		if len(group) > 0 {
			out = append(out, RPCSection{Title: title, Endpoints: group})
		}
	}
	return out
}

// RPCDown counts endpoints with no responder: the operator's at-a-glance alarm.
func RPCDown(eps []RPCEndpoint) int {
	n := 0
	for _, e := range eps {
		if !e.Reachable {
			n++
		}
	}
	return n
}
