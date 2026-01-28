package event

import (
	"time"

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

func newID() (string, error) {
	id, err := uuid.NewV7()

	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func NewBaseEvent(eventType string, priority bool) (BaseEvent, error) {
	id, err := newID()
	if err != nil {
		return BaseEvent{}, err
	}

	ms := uint64(id[5]) | uint64(id[4])<<8 | uint64(id[3])<<16 |
		uint64(id[2])<<24 | uint64(id[1])<<32 | uint64(id[0])<<40

	return BaseEvent{
		eventType: eventType,
		priority:  priority,
		id:        id,
		timeStamp: time.UnixMilli(int64(ms)).UTC(),
	}, nil
}
