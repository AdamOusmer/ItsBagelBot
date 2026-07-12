package bus

import (
	"context"
	"testing"
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
