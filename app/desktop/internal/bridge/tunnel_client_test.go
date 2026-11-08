// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
	"ItsBagelBot/app/desktop/internal/bridge/transport"
	"ItsBagelBot/app/desktop/internal/network"
	shared "ItsBagelBot/pkg/websocket"
)

type tunnelResults struct {
	mu        sync.Mutex
	sequences []uint64
	last      uint64
	dials     atomic.Int32
}

func (r *tunnelResults) Apply(event *protocol.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.Sequence != r.last+1 || event.Chat == nil || event.Chat.Text != "event-"+strconv.FormatUint(event.Sequence, 10) {
		return errors.New("tunnel sequence or typed payload mismatch")
	}
	r.last = event.Sequence
	r.sequences = append(r.sequences, event.Sequence)
	return nil
}
func (r *tunnelResults) snapshot() (uint64, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last, len(r.sequences)
}

func tunnelEndpoint(t *testing.T) (string, string) {
	t.Helper()
	endpoint := os.Getenv("BAGEL_TUNNEL_ENDPOINT")
	parsed, err := url.Parse(endpoint)
	if err != nil || !validTunnelProbeURL(parsed) {
		t.Fatal("live test requires the existing health hostname with isolated probe routes")
	}
	path := os.Getenv("BAGEL_TUNNEL_TOKEN_FILE")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("probe token file must be owner-readable only")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 32 || len(raw) > 8192 {
		t.Fatal("probe credential file unavailable or invalid")
	}
	return endpoint, strings.TrimSpace(string(raw))
}

func validTunnelProbeURL(parsed *url.URL) bool {
	return parsed.Scheme == "https" && parsed.Host == "health.itsbagelbot.com" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && !parsed.ForceQuery
}

func verifyTunnelEdge(t *testing.T, endpoint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/healthz", nil)
	if err != nil {
		t.Fatal("invalid probe health request")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return shared.ErrRedirect }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("verified public TLS probe health failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Header.Get("Cf-Ray") == "" || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatal("probe did not traverse a verified Cloudflare HTTPS endpoint")
	}
	t.Log("public Cloudflare edge verified; origin TLS remains pinned at Traefik")
}

func rejectAnonymousTunnelUpgrade(t *testing.T, endpoint string) {
	t.Helper()
	client, err := shared.NewClient(shared.Config{Endpoint: endpoint, MaxMessageBytes: protocol.MaxFrameBytes, Subprotocol: protocol.Subprotocol})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, protocol.EventsPath, "invalid-disposable-probe-token")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("unauthorized public probe upgrade succeeded")
	}
	if !errors.Is(err, shared.ErrAuthentication) {
		t.Fatalf("public unauthorized upgrade classification: %v", err)
	}
}

func startTunnelRuntime(t *testing.T, endpoint, token string, observed *tunnelResults) *Runtime {
	t.Helper()
	client, err := tunnelNativeTransport(t, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	dial := func(ctx context.Context, request transport.Request) (transport.Conn, error) {
		observed.dials.Add(1)
		return client.Dial(ctx, request)
	}
	r, err := New(Config{Endpoint: endpoint, Tokens: runtimeTokenFunc(func(context.Context) (string, error) { return token, nil }), Consumer: observed, Dial: dial, Control: client})
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	go r.Run(ctx)
	t.Cleanup(func() {
		cancel()
		wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := r.Wait(wait); err != nil {
			t.Error(err)
		}
		_ = client.Close()
	})
	if err := r.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}
func tunnelNativeTransport(t *testing.T, endpoint string) (*transport.Client, error) {
	t.Helper()
	helper := os.Getenv("BAGEL_TEST_SANDBOX_WORKER")
	if helper == "" {
		return transport.NewDirectClient(transport.Config{Endpoint: endpoint})
	}
	key := tunnelProofKey(t)
	if key == nil {
		t.Fatal("sandboxed Tunnel test requires an installation proof key")
	}
	worker, err := network.Start(t.Context(), network.Config{Executable: helper, AuthEndpoint: endpoint, BridgeEndpoint: endpoint})
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = worker.Close() })
	t.Log("OS sandbox verified; TLS/system trust, WebSocket and protobuf parsing use the isolated worker; DPoP key remains in parent")
	return transport.NewClient(transport.Config{Endpoint: endpoint, Native: worker.Bridge(), Proof: key.Proof})
}

func TestExistingTunnelWebSocketProtobuf(t *testing.T) {
	if os.Getenv("BAGEL_TUNNEL_CLIENT") != "1" {
		t.Skip("opt-in existing Tunnel integration")
	}
	endpoint, token := tunnelEndpoint(t)
	verifyTunnelEdge(t, endpoint)
	rejectAnonymousTunnelUpgrade(t, endpoint)
	observed := &tunnelResults{}
	r := startTunnelRuntime(t, endpoint, token, observed)
	started := time.Now()
	waitTunnelSequence(t, r, observed)
	last, count := observed.snapshot()
	if last != 8 || count != 8 || observed.dials.Load() != 3 {
		t.Fatalf("tunnel delivery summary: cursor=%d count=%d socket_attempts=%d", last, count, observed.dials.Load())
	}
	if time.Since(started) < 150*time.Second {
		t.Fatal("probe skipped its full keepalive-only idle soak")
	}
	t.Logf("PASS: 8 contiguous protobuf events; 2 handoffs; 3 socket attempts; elapsed %.1fs", time.Since(started).Seconds())
}

func waitTunnelSequence(t *testing.T, r *Runtime, observed *tunnelResults) {
	t.Helper()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	report := time.NewTicker(30 * time.Second)
	defer report.Stop()
	for {
		select {
		case <-tick.C:
			if tunnelProgress(t, r, observed) {
				return
			}
		case <-report.C:
			t.Logf("tunnel soak: cursor=%d socket_attempts=%d", r.Status().Cursor, observed.dials.Load())
		case <-deadline.C:
			t.Fatal("existing Tunnel probe did not finish")
		case <-t.Context().Done():
			t.Fatal("existing Tunnel probe context ended")
		}
	}
}

func tunnelProgress(t *testing.T, r *Runtime, observed *tunnelResults) bool {
	t.Helper()
	view := r.Status()
	if view.State == session.Failed || view.State == session.AuthenticationRequired || view.State == session.Reconnecting || observed.dials.Load() > 3 {
		t.Fatalf("unexpected tunnel recovery: state=%d problem=%d attempts=%d cursor=%d", view.State, view.Problem, observed.dials.Load(), view.Cursor)
	}
	return view.State == session.Online && view.Cursor == 8
}
