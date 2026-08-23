// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// seqRecorder collects the order tasks actually ran in, safe because the
// Sequencer itself serializes the runs it orders.
type seqRecorder struct {
	mu  sync.Mutex
	ran []string
}

func (r *seqRecorder) record(name string) {
	r.mu.Lock()
	r.ran = append(r.ran, name)
	r.mu.Unlock()
}

func (r *seqRecorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ran...)
}

func TestSequencerRunsPerBroadcasterInArrivalOrder(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	for i := range 20 {
		name := "task" + string(rune('a'+i))
		s.Do(7, func() { rec.record(name) })
	}
	assert.Eventually(t, func() bool { return len(rec.names()) == 20 }, time.Second, time.Millisecond)
	want := make([]string, 0, 20)
	for i := range 20 {
		want = append(want, "task"+string(rune('a'+i)))
	}
	assert.Equal(t, want, rec.names(), "tasks must run strictly in enqueue order")
}

func TestSequencerKeepsBroadcasterQueuesIndependent(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	var wg sync.WaitGroup
	for _, id := range []uint64{1, 2, 3, 4} {
		for i := range 10 {
			wg.Add(1)
			s.Do(id, func() {
				rec.record(string(rune('a'+id-1)) + string(rune('a'+i)))
				wg.Done()
			})
		}
	}
	wg.Wait()
	names := rec.names()
	assert.Len(t, names, 40)
	// Within one broadcaster's slice of the log the relative order must hold;
	// interleaving across broadcasters is allowed and irrelevant.
	for _, id := range []uint64{1, 2, 3, 4} {
		prefix := string(rune('a' + id - 1))
		var got []string
		for _, n := range names {
			if len(n) == 2 && n[0] == prefix[0] {
				got = append(got, n[1:])
			}
		}
		want := make([]string, 0, 10)
		for i := range 10 {
			want = append(want, string(rune('a'+i)))
		}
		assert.Equal(t, want, got, "broadcaster %d lost ordering", id)
	}
}

func TestSequencerWaitsForSlowTaskBeforeStartingNext(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	release := make(chan struct{})
	s.Do(9, func() {
		rec.record("slow")
		<-release
	})
	s.Do(9, func() { rec.record("after") })
	// The second task must not start while the first is still blocked.
	assert.Never(t, func() bool { return len(rec.names()) > 1 }, 50*time.Millisecond, 5*time.Millisecond,
		"task ran before its predecessor completed")
	close(release)
	assert.Eventually(t, func() bool { return len(rec.names()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"slow", "after"}, rec.names())
}

func TestSequencerRespawnsPumpAfterIdle(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	s.Do(5, func() { rec.record("first") })
	assert.Eventually(t, func() bool { return len(rec.names()) == 1 }, time.Second, time.Millisecond)
	// The pump exits once the queue drains; a later event must still run.
	assert.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.seqs) == 0
	}, time.Second, time.Millisecond, "drained queue must be reaped")
	s.Do(5, func() { rec.record("second") })
	assert.Eventually(t, func() bool { return len(rec.names()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"first", "second"}, rec.names())
}

func TestSequencerIgnoresZeroIDAndNilTask(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	assert.NotPanics(t, func() {
		s.Do(0, func() { rec.record("zero-id") })
		s.Do(3, nil)
	})
	// Only a task with a real broadcaster id and a body may ever run.
	s.Do(3, func() { rec.record("real") })
	assert.Eventually(t, func() bool { return len(rec.names()) == 1 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"real"}, rec.names())
}

func TestSequencerConcurrentDoIsSafe(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Do(uint64(1+i%4), func() { rec.record("t") })
		}(i)
	}
	wg.Wait()
	assert.Eventually(t, func() bool { return len(rec.names()) == 50 }, time.Second, time.Millisecond)
}
