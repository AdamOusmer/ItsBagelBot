package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestCloneWithoutBatchHeadersKeepsDedupAndMetadata(t *testing.T) {
	src := &nats.Msg{
		Subject: "data.user.changed",
		Data:    []byte(`{"id":1}`),
		Reply:   "_INBOX.batch",
		Header: nats.Header{
			nats.MsgIdHdr:       []string{"msg-1"},
			"traceparent":       []string{"00-trace"},
			batchIDHeader:       []string{"batch-1"},
			batchSequenceHeader: []string{"2"},
			batchCommitHeader:   []string{"1"},
			requiredAPIHeader:   []string{"2"},
		},
	}

	got := cloneWithoutBatchHeaders(src)
	if got.Reply != "" {
		t.Fatalf("fallback reply = %q, want empty", got.Reply)
	}
	if got.Header.Get(nats.MsgIdHdr) != "msg-1" || got.Header.Get("traceparent") != "00-trace" {
		t.Fatalf("fallback lost application headers: %#v", got.Header)
	}
	for _, header := range []string{batchIDHeader, batchSequenceHeader, batchCommitHeader, requiredAPIHeader} {
		if got.Header.Get(header) != "" {
			t.Fatalf("fallback retained %s", header)
		}
	}
	if src.Header.Get(batchIDHeader) != "batch-1" {
		t.Fatal("clone mutated the original message")
	}
}
