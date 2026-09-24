// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
)

const (
	seqQueueDepth = 64
)

type Sequencer struct {
	mu   sync.Mutex
	seqs map[uint64]*seqQueue
}

type seqQueue struct {
	ch   chan func()
	busy bool
}

func NewSequencer() *Sequencer {
	return &Sequencer{seqs: make(map[uint64]*seqQueue)}
}

func (s *Sequencer) Do(broadcasterID uint64, task func()) {
	if broadcasterID == 0 || task == nil {
		return
	}
	s.mu.Lock()
	q, ok := s.seqs[broadcasterID]
	if !ok {
		q = &seqQueue{ch: make(chan func(), seqQueueDepth)}
		s.seqs[broadcasterID] = q
	}
	select {
	case q.ch <- task:
		if !q.busy {
			q.busy = true
			s.mu.Unlock()
			go s.pump(broadcasterID, q)
			return
		}
		s.mu.Unlock()
	default:
		s.mu.Unlock()
		task()
	}
}

func (s *Sequencer) pump(broadcasterID uint64, q *seqQueue) {
	for {
		var task func()
		s.mu.Lock()
		select {
		case task = <-q.ch:
			s.mu.Unlock()
		default:
			delete(s.seqs, broadcasterID)
			q.busy = false
			s.mu.Unlock()
			return
		}
		task()
	}
}
