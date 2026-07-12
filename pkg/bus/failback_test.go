package bus

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsLocalLeaf(t *testing.T) {
	tests := []struct {
		name   string
		server string
		node   string
		want   bool
	}{
		{name: "same node", server: "node1--nats-leaf-abc", node: "node1", want: true},
		{name: "other node", server: "node2--nats-leaf-def", node: "node1", want: false},
		{name: "prefix collision", server: "node10--nats-leaf-def", node: "node1", want: false},
		{name: "disabled without node", server: "node1--nats-leaf-abc", node: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalLeaf(tt.server, tt.node); got != tt.want {
				t.Fatalf("isLocalLeaf(%q, %q) = %v, want %v", tt.server, tt.node, got, tt.want)
			}
		})
	}
}

func TestLocalLeafReadyUsesMonitorHealth(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if !localLeafReady(server.URL+"/healthz", time.Second) {
		t.Fatal("expected healthy monitor endpoint to be ready")
	}
	if localLeafReady(server.URL+"/missing", time.Second) {
		t.Fatal("expected non-200 monitor endpoint to be unready")
	}
}

func TestLocalLeafReadyFailsClosed(t *testing.T) {
	t.Parallel()

	if localLeafReady("http://127.0.0.1:1/healthz", 25*time.Millisecond) {
		t.Fatal("expected unreachable monitor endpoint to be unready")
	}
}
