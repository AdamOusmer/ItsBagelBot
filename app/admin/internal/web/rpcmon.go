package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"itsbagelbot/admin/ui"
)

// rpcEndpoint is one request-reply subject the fleet relies on. RPC rides core
// NATS (not JetStream), so there is no stream/consumer telemetry for it; the
// only health signal the broker exposes is whether a responder is currently
// subscribed. We read that from the NATS HTTP monitor (/subsz?test=<subject>),
// which returns the subscription(s) that would receive a request on the
// subject, with each subscriber's delivered-message counter.
type rpcEndpoint struct {
	name    string
	subject string
	section string
}

// rpcEndpoints is the curated set of fleet RPC subjects, one per distinct
// responder service (the user-admin verbs share a single responder, so testing
// one verb covers them all). Mirrors the subscribe grants in nats-auth.conf.
var rpcEndpoints = []rpcEndpoint{
	{"Ingress shard snapshot", "twitch.ingress.admin.shards.get", "Operator"},
	{"User administration", "bagel.rpc.admin.user.get", "Operator"},
	{"Broadcaster status", "bagel.rpc.broadcaster.status.get", "Projections"},
	{"Users projection", "bagel.rpc.internal.projection.users.get", "Projections"},
	{"Commands projection", "bagel.rpc.internal.projection.commands.get", "Projections"},
	{"Modules projection", "bagel.rpc.internal.projection.modules.get", "Projections"},
	{"Bot token load", "bagel.rpc.internal.tokens.get", "Internal"},
}

// rpcMonitor scrapes the NATS monitor for responder presence, cached on a short
// TTL like the other Server telemetry. Zero value is not usable; see newRPCMonitor.
type rpcMonitor struct {
	base   string
	client *http.Client

	mu    sync.Mutex
	value []ui.RPCEndpoint
	err   string
	until time.Time
}

func newRPCMonitor(monitorURL string) *rpcMonitor {
	return &rpcMonitor{
		base:   monitorURL,
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

// subszResp mirrors the NATS /subsz response. The subscription array is keyed
// "subscriptions_list" (not "subscriptions"), and the queue group field is
// "qgroup"; getting either wrong yields an empty list and a false NO RESPONDER.
type subszResp struct {
	Subscriptions []struct {
		Subject    string `json:"subject"`
		Msgs       uint64 `json:"msgs"`
		QueueGroup string `json:"qgroup"`
	} `json:"subscriptions_list"`
}

// snapshot returns the cached RPC health, refreshing it at most once per ~3s.
func (m *rpcMonitor) snapshot() ([]ui.RPCEndpoint, string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if now := time.Now(); now.Before(m.until) {
		return m.value, m.err
	}

	// One cheap liveness call first: if the monitor is unreachable, fail fast
	// instead of waiting out the timeout on every endpoint below.
	if _, err := m.get("/varz"); err != nil {
		m.value, m.err = nil, "NATS monitor unreachable: "+err.Error()
		m.until = time.Now().Add(3 * time.Second)
		return m.value, m.err
	}

	out := make([]ui.RPCEndpoint, 0, len(rpcEndpoints))
	for _, e := range rpcEndpoints {
		ep := ui.RPCEndpoint{Name: e.name, Subject: e.subject, Section: e.section}
		body, err := m.get("/subsz?subs=1&test=" + url.QueryEscape(e.subject))
		if err == nil {
			var r subszResp
			if json.Unmarshal(body, &r) == nil {
				ep.Responders = len(r.Subscriptions)
				ep.Reachable = ep.Responders > 0
				for _, s := range r.Subscriptions {
					ep.Msgs += s.Msgs
					if ep.Queue == "" {
						ep.Queue = s.QueueGroup
					}
				}
			}
		}
		out = append(out, ep)
	}

	m.value, m.err = out, ""
	m.until = time.Now().Add(3 * time.Second)
	return m.value, m.err
}

func (m *rpcMonitor) get(path string) ([]byte, error) {
	resp, err := m.client.Get(m.base + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("monitor %s returned %d", path, resp.StatusCode)
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if rerr != nil {
			break
		}
		if len(buf) > 1<<20 {
			break
		}
	}
	return buf, nil
}
