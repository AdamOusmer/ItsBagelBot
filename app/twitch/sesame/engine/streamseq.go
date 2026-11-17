// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"sync"
)

type Sequencer struct {
	mu   sync.Mutex
	seqs map[uint64]*seqQueue
}

type seqQueue struct {
	tasks []func()
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
		q = &seqQueue{}
		s.seqs[broadcasterID] = q
	}
	q.tasks = append(q.tasks, task)
	s.mu.Unlock()
	if !ok {
		go s.pump(broadcasterID, q)
	}
}

func (s *Sequencer) pump(broadcasterID uint64, q *seqQueue) {
	for {
		s.mu.Lock()
		if len(q.tasks) == 0 {
			delete(s.seqs, broadcasterID)
			s.mu.Unlock()
			return
		}
		task := q.tasks[0]
		q.tasks[0] = nil
		q.tasks = q.tasks[1:]
		s.mu.Unlock()
		task()
	}
}
