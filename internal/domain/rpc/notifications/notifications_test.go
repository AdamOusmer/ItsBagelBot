package notificationsrpc

import (
	"encoding/json"
	"testing"
)

func TestSendRequestDecodesStringActorID(t *testing.T) {
	var req SendRequest
	if err := json.Unmarshal([]byte(`{"actor_id":"804932984","request_id":"send-123"}`), &req); err != nil {
		t.Fatalf("decode send request: %v", err)
	}
	if req.ActorID != "804932984" {
		t.Fatalf("actor id = %q, want %q", req.ActorID, "804932984")
	}
	if req.RequestID != "send-123" {
		t.Fatalf("request id = %q, want %q", req.RequestID, "send-123")
	}
}
