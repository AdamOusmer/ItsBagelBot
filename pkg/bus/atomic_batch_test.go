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

func TestAtomicSendStagesBatchFraming(t *testing.T) {
	sender := &atomicSender{prefix: "_INBOX.test.", waits: make(map[string]chan *nats.Msg)}
	batch := stagedBatch(3)

	// Frame exactly as send does, without a live connection.
	cohort := atomicCohort{id: "B1", ch: sender.begin("B1"), n: len(batch)}
	frameBatch(sender.prefix, batch, cohort.id)

	first, mid, last := batch[0].msg, batch[1].msg, batch[2].msg
	for i, msg := range []*nats.Msg{first, mid, last} {
		if msg.Header.Get(batchIDHdr) != "B1" {
			t.Fatalf("message %d lost its batch id", i)
		}
	}
	if first.Header.Get(batchSeqHdr) != "1" || mid.Header.Get(batchSeqHdr) != "2" || last.Header.Get(batchSeqHdr) != "3" {
		t.Fatal("batch sequence numbering is wrong")
	}
	if first.Reply != "_INBOX.test.s.B1" {
		t.Fatalf("batch-opening message must carry the start reply, got %q", first.Reply)
	}
	if mid.Reply != "" || mid.Header.Get(batchCommitHdr) != "" {
		t.Fatal("intermediate message must travel without reply or commit header")
	}
	if last.Header.Get(batchCommitHdr) != "1" || last.Reply != "_INBOX.test.c.B1" {
		t.Fatal("final message must commit the batch and carry the commit reply")
	}

	stripBatchHeaders(batch)
	for i, req := range batch {
		if req.msg.Reply != "" ||
			req.msg.Header.Get(batchIDHdr) != "" ||
			req.msg.Header.Get(batchSeqHdr) != "" ||
			req.msg.Header.Get(batchCommitHdr) != "" {
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
