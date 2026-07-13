package bus

import (
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestPublishWireModeDefaultsToSingle(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "")
	if publishWireMode() != wireSingle {
		t.Fatal("unset NATS_PUBLISH_WIRE must select the per-message PubAck wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "nonsense")
	if publishWireMode() != wireSingle {
		t.Fatal("unknown NATS_PUBLISH_WIRE must select the per-message PubAck wire")
	}
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	if publishWireMode() != wireAtomic {
		t.Fatal("NATS_PUBLISH_WIRE=atomic must select the atomic batch wire")
	}
}

// framedBatch stages a 3-message cohort and frames it exactly as send does,
// without a live connection.
func framedBatch() []publishRequest {
	batch := stagedBatch(3)
	frameBatch("_INBOX.test.", batch, "B1")
	return batch
}

func TestFrameBatchStampsHeadersAndReplies(t *testing.T) {
	batch := framedBatch()

	first, mid, last := batch[0].msg, batch[1].msg, batch[2].msg
	for i, want := range []struct {
		msg    *nats.Msg
		seq    string
		commit string
		reply  string
	}{
		{first, "1", "", "_INBOX.test.s.B1"},
		{mid, "2", "", ""},
		{last, "3", "1", "_INBOX.test.c.B1"},
	} {
		if want.msg.Header.Get(batchIDHdr) != "B1" {
			t.Fatalf("message %d lost its batch id", i)
		}
		if got := want.msg.Header.Get(batchSeqHdr); got != want.seq {
			t.Fatalf("message %d sequence %q, want %q", i, got, want.seq)
		}
		if got := want.msg.Header.Get(batchCommitHdr); got != want.commit {
			t.Fatalf("message %d commit header %q, want %q", i, got, want.commit)
		}
		if want.msg.Reply != want.reply {
			t.Fatalf("message %d reply %q, want %q", i, want.msg.Reply, want.reply)
		}
	}
}

func TestStripBatchHeadersRevertsFramingKeepsDedup(t *testing.T) {
	batch := framedBatch()
	stripBatchHeaders(batch)

	for i, req := range batch {
		framed := req.msg.Reply != "" ||
			req.msg.Header.Get(batchIDHdr) != "" ||
			req.msg.Header.Get(batchSeqHdr) != "" ||
			req.msg.Header.Get(batchCommitHdr) != ""
		if framed {
			t.Fatalf("message %d kept batch framing after strip", i)
		}
		if req.msg.Header.Get(nats.MsgIdHdr) == "" {
			t.Fatalf("message %d lost its dedup id during strip", i)
		}
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}

func stagedBatch(n int) []publishRequest {
	batch := make([]publishRequest, 0, n)
	for i := 0; i < n; i++ {
		msg := nats.NewMsg("data.test.batch")
		msg.Header.Set(nats.MsgIdHdr, "id-"+itoa(i+1))
		batch = append(batch, publishRequest{msg: msg, msgID: "id-" + itoa(i+1)})
	}
	return batch
}

func TestAtomicRouteFiltersStartAcks(t *testing.T) {
	sender := &atomicSender{prefix: "_INBOX.test.", waits: make(map[string]chan *nats.Msg)}
	ch := sender.begin("B1")
	defer sender.end("B1")

	// Zero-byte start ack: success signal only, must not resolve the cohort.
	sender.route(&nats.Msg{Subject: "_INBOX.test.s.B1"})
	select {
	case <-ch:
		t.Fatal("zero-byte start ack must be filtered")
	default:
	}

	// A start error must reach the waiter.
	sender.route(&nats.Msg{Subject: "_INBOX.test.s.B1", Data: []byte(`{"error":{"code":400,"err_code":10174,"description":"batch publish not enabled"}}`)})
	select {
	case msg := <-ch:
		err := checkBatchAck(msg.Data, atomicCohort{id: "B1", n: 2})
		var apiErr *batchAckAPIErr
		if !errors.As(err, &apiErr) || apiErr.ErrCode != 10174 {
			t.Fatalf("start error did not surface as a batch API error: %v", err)
		}
	default:
		t.Fatal("start error was not routed to the cohort")
	}

	// Replies for unknown cohorts and foreign subjects are dropped silently.
	sender.route(&nats.Msg{Subject: "_INBOX.test.c.UNKNOWN", Data: []byte(`{}`)})
	sender.route(&nats.Msg{Subject: "other.subject", Data: []byte(`{}`)})
}

func TestCheckBatchAck(t *testing.T) {
	cohort := atomicCohort{id: "B1", n: 3}

	if err := checkBatchAck([]byte(`{"stream":"BAGEL_DATA","seq":42,"batch":"B1","count":3}`), cohort); err != nil {
		t.Fatalf("valid commit ack rejected: %v", err)
	}
	if err := checkBatchAck([]byte(`{"error":{"code":400,"err_code":10201,"description":"duplicate message id"}}`), cohort); err == nil {
		t.Fatal("error ack must fail the cohort")
	}
	if err := checkBatchAck([]byte(`{"stream":"BAGEL_DATA","seq":42,"batch":"OTHER","count":3}`), cohort); err == nil {
		t.Fatal("commit ack for a different batch must fail the cohort")
	}
	if err := checkBatchAck([]byte(`{"stream":"BAGEL_DATA","seq":42,"batch":"B1","count":2}`), cohort); err == nil {
		t.Fatal("commit ack covering fewer messages must fail the cohort")
	}
	if err := checkBatchAck([]byte(`{}`), cohort); err == nil {
		t.Fatal("ack without a stream sequence must fail the cohort")
	}
	if err := checkBatchAck([]byte(`not json`), cohort); err == nil {
		t.Fatal("undecodable ack must fail the cohort")
	}
}
