// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/oauth"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/dpop"
	"ItsBagelBot/pkg/protobuf"
	shared "ItsBagelBot/pkg/websocket"
	"ItsBagelBot/pkg/websocket/testkit"
)

func realWorker(t *testing.T) string {
	t.Helper()
	path := os.Getenv("BAGEL_TEST_SANDBOX_WORKER")
	if path == "" {
		t.Skip("opt-in real OS-sandboxed network worker")
	}
	return path
}
func isolatedFixture(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.TLS.Certificates[0].Certificate[0]})
	// This test-only dialer permits the controlled loopback fixture. Production
	// construction always resolves and dials the same validated public address.
	client, err := newClient(t.Context(), Config{Executable: realWorker(t), AuthEndpoint: server.URL, BridgeEndpoint: server.URL, RootsPEM: certificate}, func(b *broker) {
		b.dial = func(ctx context.Context, origin string) (net.Conn, error) {
			return new(net.Dialer).DialContext(ctx, "tcp", server.Listener.Addr().String())
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
func TestSandboxWorkerTLSAndOAuthParsing(t *testing.T) {
	_ = realWorker(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != oauth.TokenPath || r.Header.Get("DPoP") == "" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body, _ := codec.Marshal(oauth.TokenReply{Access: "v2.synthetic", Type: "DPoP", Expires: 86400, Refresh: "bbr_rotated", Grant: "grant", Device: "device"})
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := isolatedFixture(t, server)
	key, err := dpop.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	proof, err := key.Proof("POST", server.URL+oauth.TokenPath, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	reply, err := client.Do(ctx, oauth.NetworkRequest{Endpoint: server.URL + oauth.TokenPath, Proof: proof, Form: "client_id=itsbagelbot-desktop&grant_type=refresh_token&refresh_token=bbr_synthetic"})
	if err != nil || reply.Status != 200 || reply.Token.Access != "v2.synthetic" || reply.Token.Type != "DPoP" {
		t.Fatalf("isolated OAuth result status=%d error=%v", reply.Status, err)
	}
	t.Log("real sandbox; parent-owned raw TCP; TLS and OAuth JSON parsed in isolated worker")
}
func TestSandboxWorkerTypedStreamLease(t *testing.T) {
	_ = realWorker(t)
	service := testkit.Service{StreamPath: protocol.EventsPath, Subprotocol: protocol.Subprotocol, HandleStream: func(ctx context.Context, _ testkit.Call, send func([]byte) error) error {
		message := &protocol.Message{Version: protocol.Version, Kind: protocol.SessionStarted, SessionId: "isolated-session", BindWithinMs: 5000}
		raw := make([]byte, 128)
		n, err := protobuf.MarshalTo(raw, message)
		if err != nil {
			return err
		}
		if err = send(raw[:n]); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}}
	server := httptest.NewTLSServer(testkit.Handler(service))
	defer server.Close()
	client := isolatedFixture(t, server)
	key, err := dpop.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	proof, _ := key.Proof("GET", server.URL+protocol.EventsPath, "v2.synthetic", "")
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	stream, reply, err := client.DialAuthorized(ctx, protocol.EventsPath, shared.Authorization{Scheme: "DPoP", Token: "v2.synthetic", Proof: proof})
	if err != nil || reply.Status != 101 {
		t.Fatal("isolated socket upgrade", err)
	}
	defer stream.Close()
	message := protocol.ServerMessageFromVTPool()
	defer message.ReturnToVTPool()
	if err = stream.Recv(ctx, message); err != nil || message.SessionId != "isolated-session" {
		t.Fatal("isolated typed welcome", err)
	}
	t.Log("real sandbox; binary WebSocket decoded and validated before bounded typed IPC")
}
