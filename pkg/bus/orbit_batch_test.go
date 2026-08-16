// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"strconv"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/synadia-io/orbit.go/jetstreamext"
	"go.uber.org/zap"
)

func TestStripOrbitBatchFramingKeepsFleetIdentity(t *testing.T) {
	batch := framedBatch(2)

	stripOrbitBatchFraming(batch)

	for i := range batch {
		assertBatchFramingStripped(t, batch[i].msg)
		assertMessageIdentityPreserved(t, batch[i].msg)
		assertBrokerDedupAbsent(t, batch[i].msg)
	}
}

func TestCollectBatchUsesTheWireCohortShape(t *testing.T) {
	worker := &publishBatchWorker{
		requests:  make(chan publishRequest, 4),
		stop:      make(chan struct{}),
		batchSize: 2,
		batchWait: time.Second,
	}
	staged := stagedBatch(3)
	worker.requests <- staged[1]
	worker.requests <- staged[2]

	batch, ok := worker.collectBatch(staged[0])
	if !ok || len(batch) != 2 {
		t.Fatalf("collectBatch() = (%d, %v), want the configured cohort size 2", len(batch), ok)
	}
}

func TestCollectBatchStopsAtTheWireWait(t *testing.T) {
	worker := &publishBatchWorker{
		requests:  make(chan publishRequest, 1),
		stop:      make(chan struct{}),
		batchSize: defaultPublishBatchSize,
		batchWait: time.Millisecond,
	}

	batch, ok := worker.collectBatch(stagedBatch(1)[0])
	if !ok || len(batch) != 1 {
		t.Fatalf("collectBatch() = (%d, %v), want the single staged message", len(batch), ok)
	}
}

func assertBatchFramingStripped(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Reply != "" {
		t.Fatalf("kept batch reply %q after strip", msg.Reply)
	}
	for _, header := range []string{
		jetstreamext.BatchIDHeader,
		jetstreamext.BatchSeqHeader,
		jetstreamext.BatchCommitHeader,
	} {
		if value := msg.Header.Get(header); value != "" {
			t.Fatalf("kept batch header %s=%q after strip", header, value)
		}
	}
}

func assertMessageIdentityPreserved(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Header.Get(messageIDHeader) == "" {
		t.Fatal("lost its fleet message id during strip")
	}
	if msg.Header.Get(legacyMessageIDHeader) == "" {
		t.Fatal("lost its rollout compatibility id during strip")
	}
}

func assertBrokerDedupAbsent(t *testing.T, msg *nats.Msg) {
	t.Helper()
	if msg.Header.Get(nats.MsgIdHdr) != "" {
		t.Fatal("unexpectedly carries broker dedup")
	}
}

// framedBatch stages a cohort already carrying Orbit's batch framing, as it
// looks when a rejected batch reaches the fallback.
func framedBatch(n int) []publishRequest {
	batch := stagedBatch(n)
	for i := range batch {
		batch[i].msg.Header.Set(jetstreamext.BatchIDHeader, "B1")
		batch[i].msg.Header.Set(jetstreamext.BatchSeqHeader, itoa(i+1))
		batch[i].msg.Reply = "_INBOX.batch"
	}
	batch[len(batch)-1].msg.Header.Set(jetstreamext.BatchCommitHeader, "1")
	return batch
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func stagedBatch(n int) []publishRequest {
	batch := make([]publishRequest, 0, n)
	for i := 0; i < n; i++ {
		msg := nats.NewMsg("data.test.batch")
		msg.Header.Set(messageIDHeader, "id-"+itoa(i+1))
		msg.Header.Set(legacyMessageIDHeader, "id-"+itoa(i+1))
		batch = append(batch, publishRequest{msg: msg})
	}
	return batch
}

// confirmedBatch stages a cohort whose callers are all waiting on a per-message
// verdict, as PublishConfirmed leaves them.
func confirmedBatch(n int) []publishRequest {
	batch := stagedBatch(n)
	for i := range batch {
		batch[i].confirmed = make(chan error, 1)
	}
	return batch
}

// newTestBatchPublisher builds the connection-side state cohort resolution
// touches, without a broker connection.
func newTestBatchPublisher() *batchPublisher {
	return &batchPublisher{log: zap.NewNop(), changed: make(chan struct{})}
}
