// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newOverlapWorker() *publishBatchWorker {
	owner := newTestBatchPublisher()
	owner.wire = wireAtomic
	return &publishBatchWorker{
		owner:         owner,
		requests:      make(chan publishRequest, 8),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		slots:         make(chan struct{}, 4),
		batchSize:     defaultAtomicPublishBatchSize,
		batchWait:     defaultPublishBatchWait,
		overlapCommit: true,
	}
}

func TestCollectBatchKeepsFillingWhileSlotsAreBusy(t *testing.T) {
	worker := newOverlapWorker()
	worker.batchWait = time.Millisecond
	worker.batchSize = 8
	worker.slots = make(chan struct{}, 1)
	worker.slots <- struct{}{}

	staged := stagedBatch(4)
	done := make(chan []publishRequest, 1)
	go func() {
		batch, _ := worker.collectBatch(staged[0])
		done <- batch
	}()
	time.Sleep(20 * time.Millisecond)
	worker.requests <- staged[1]
	worker.requests <- staged[2]
	time.Sleep(5 * time.Millisecond)
	select {
	case batch := <-done:
		t.Fatalf("cohort of %d closed while the slot was busy", len(batch))
	default:
	}
	<-worker.slots

	batch := <-done

	assert.Len(t, batch, 3, "the cohort must hold the 3 messages that arrived during the slot wait")
	assert.True(t, worker.slotHeld, "collectTimed returned without the slot it waited for")
	assert.Len(t, worker.slots, 1)
}

func TestCollectBatchUngatedWireClosesOnTheWindow(t *testing.T) {
	worker := newOverlapWorker()
	worker.overlapCommit = false
	worker.batchWait = time.Millisecond
	for range cap(worker.slots) {
		worker.slots <- struct{}{}
	}

	batch, ok := worker.collectBatch(stagedBatch(1)[0])

	assert.True(t, ok)
	assert.Len(t, batch, 1, "the window must close the cohort")
	assert.False(t, worker.slotHeld, "an ungated wire must not take an inflight slot")
}
