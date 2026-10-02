// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type seqRecorder struct {
	mu  sync.Mutex
	ran []string
}

func (r *seqRecorder) noteRan(name string) {
	r.mu.Lock()
	r.ran = append(r.ran, name)
	r.mu.Unlock()
}

func (r *seqRecorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.ran...)
}

func (r *seqRecorder) namesFor(broadcaster uint64) []string {
	prefix := strconv.FormatUint(broadcaster, 10) + ":"
	return slices.DeleteFunc(r.names(), func(name string) bool { return !strings.HasPrefix(name, prefix) })
}

type numberedRun struct {
	seq *Sequencer
	rec *seqRecorder
	wg  sync.WaitGroup
}

func (r *numberedRun) enqueue(broadcaster uint64, count int) []string {
	want := make([]string, count)
	for i := range count {
		name := strconv.FormatUint(broadcaster, 10) + ":" + strconv.Itoa(i)
		want[i] = name
		r.wg.Add(1)
		r.seq.Do(broadcaster, func() {
			r.rec.noteRan(name)
			r.wg.Done()
		})
	}
	return want
}

func TestSequencerRunsEachBroadcasterInArrivalOrder(t *testing.T) {
	run := &numberedRun{seq: NewSequencer(), rec: &seqRecorder{}}
	want := map[uint64][]string{}
	for _, id := range []uint64{1, 2, 3, 4} {
		want[id] = run.enqueue(id, 20)
	}
	run.wg.Wait()

	for id, names := range want {
		assert.Equal(t, names, run.rec.namesFor(id), "broadcaster %d lost ordering", id)
	}
}

func TestSequencerWaitsForSlowTaskBeforeStartingNext(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	release := make(chan struct{})
	s.Do(9, func() {
		rec.noteRan("slow")
		<-release
	})
	s.Do(9, func() { rec.noteRan("after") })
	assert.Never(t, func() bool { return len(rec.names()) > 1 }, 50*time.Millisecond, 5*time.Millisecond,
		"task ran before its predecessor completed")
	close(release)
	assert.Eventually(t, func() bool { return len(rec.names()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"slow", "after"}, rec.names())
}

func TestSequencerRespawnsPumpAfterIdle(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	s.Do(5, func() { rec.noteRan("first") })
	assert.Eventually(t, func() bool { return len(rec.names()) == 1 }, time.Second, time.Millisecond)
	assert.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.seqs) == 0
	}, time.Second, time.Millisecond, "drained queue must be reaped")
	s.Do(5, func() { rec.noteRan("second") })
	assert.Eventually(t, func() bool { return len(rec.names()) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []string{"first", "second"}, rec.names())
}

func TestSequencerIgnoresZeroIDAndNilTask(t *testing.T) {
	s := NewSequencer()
	rec := &seqRecorder{}
	assert.NotPanics(t, func() {
		s.Do(0, func() { rec.noteRan("zero-id") })
		s.Do(3, nil)
	})
	s.Do(3, func() { rec.noteRan("real") })
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
			s.Do(uint64(1+i%4), func() { rec.noteRan("t") })
		}(i)
	}
	wg.Wait()
	assert.Eventually(t, func() bool { return len(rec.names()) == 50 }, time.Second, time.Millisecond)
}
