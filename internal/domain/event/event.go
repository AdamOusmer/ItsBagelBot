// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package event

import (
	"time"

	"ItsBagelBot/internal/utils"

	"github.com/google/uuid"
)

type Event interface {
	EventType() string
	Priority() bool
	ID() string
	TimeStamp() time.Time
}

type BaseEvent struct {
	eventType string
	priority  bool
	id        string
	timeStamp time.Time
}

func (e BaseEvent) ID() string           { return e.id }
func (e BaseEvent) EventType() string    { return e.eventType }
func (e BaseEvent) Priority() bool       { return e.priority }
func (e BaseEvent) TimeStamp() time.Time { return e.timeStamp }

func NewBaseEvent(eventType string, priority bool) (BaseEvent, error) {
	id, err := utils.NewID()
	if err != nil {
		return BaseEvent{}, err
	}

	return BaseEvent{
		eventType: eventType,
		priority:  priority,
		id:        id.String(),
		timeStamp: time.UnixMilli(int64(uuidV7UnixMillis(id))).UTC(),
	}, nil
}

func uuidV7UnixMillis(id uuid.UUID) uint64 {
	return uint64(id[5]) | uint64(id[4])<<8 | uint64(id[3])<<16 |
		uint64(id[2])<<24 | uint64(id[1])<<32 | uint64(id[0])<<40
}
