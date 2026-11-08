// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bridge

import (
	"ItsBagelBot/app/desktop/internal/bridge/protocol"
	"ItsBagelBot/app/desktop/internal/bridge/session"
)

const pendingCapacity = 16

// Queue entries own detached VT event leases. Enqueue/dequeue are O(1); reset
// releases at most sixteen events and their bounded decoded payloads.
type pendingFrames struct {
	id          session.Connection
	events      [pendingCapacity]*protocol.Event
	head, count int
}

func (q *pendingFrames) reset() {
	for q.count > 0 {
		q.pop().ReturnToVTPool()
	}
	q.id = 0
	q.head = 0
}
func (q *pendingFrames) push(id session.Connection, event *protocol.Event) bool {
	if event == nil || q.count == pendingCapacity || (q.count > 0 && q.id != id) {
		return false
	}
	q.id = id
	q.events[(q.head+q.count)%pendingCapacity] = event
	q.count++
	return true
}
func (q *pendingFrames) pop() *protocol.Event {
	event := q.events[q.head]
	q.events[q.head] = nil
	q.head = (q.head + 1) % pendingCapacity
	q.count--
	return event
}
