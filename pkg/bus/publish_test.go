package bus

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestPublishPartitionIsScopedAndStable(t *testing.T) {
	base := context.Background()
	if got := publishPartition(base); got != "" {
		t.Fatalf("base partition = %q, want empty", got)
	}
	ctx := WithPublishPartition(base, "channel-123")
	if got := publishPartition(ctx); got != "channel-123" {
		t.Fatalf("partition = %q, want channel-123", got)
	}
	router := hashStreamRouter{}
	first := router.Connection("TWITCH_OUTGRESS\x00"+publishPartition(ctx), 4)
	second := router.Connection("TWITCH_OUTGRESS\x00"+publishPartition(ctx), 4)
	if first != second {
		t.Fatalf("same aggregate moved connections: %d != %d", first, second)
	}
}

func TestPublishMessageUsesFleetIdentityWithoutBrokerDedup(t *testing.T) {
	msg := publishMessage(publishCommand{
		ctx: context.Background(), topic: "data.test", msgID: "event-42", payload: []byte("{}"),
	})

	if got := msg.Header.Get(messageIDHeader); got != "event-42" {
		t.Fatalf("fleet message id = %q, want event-42", got)
	}
	if got := msg.Header.Get(legacyMessageIDHeader); got != "event-42" {
		t.Fatalf("rolling-deploy compatibility id = %q, want event-42", got)
	}
	if got := msg.Header.Get(nats.MsgIdHdr); got != "" {
		t.Fatalf("broker dedup header unexpectedly set to %q", got)
	}
}
