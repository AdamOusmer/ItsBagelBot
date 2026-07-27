package main

import (
	"strconv"
	"strings"

	"github.com/bytedance/sonic"
)

// eventClass decides whether an event reaches the idempotency guard at all, and
// what the consumer is entitled to conclude when it does.
//
// Only the two guard-sampled classes touch Valkey, which is what keeps the
// claim rate at production shape instead of putting one SET NX in front of the
// whole firehose: sesame guards a handful of side-effecting handlers, not every
// chat line, so a rig that claimed every event would measure a system nobody
// runs and would put the live Valkey primary under a load nothing justifies.
type eventClass uint8

const (
	// classPlain is the firehose: decoded, counted, never claimed.
	classPlain eventClass = iota
	// classControl is guard-sampled and published exactly once. Any duplicate
	// verdict on it is a false positive, because nothing ever replayed it.
	classControl
	// classTreatment is guard-sampled and published twice under one event id.
	// It is the guard's test vector: the second copy must be caught.
	classTreatment
)

// envelope is one lane event. It is JSON because the lanes it stands in for are
// JSON EventSub payloads and the encoder cost belongs in the measurement, and it
// is small because the size that matters is set by Pad rather than by the field
// list.
//
// EventID and Seq are deliberately different identities. Seq is the wire
// position: one per published message, strictly increasing per (publisher,
// lane), which is what lets the consumer separate a delivery gap from a
// redelivery with two counters and no per-message state. EventID is the logical
// identity two copies of a duplicated event share, which is what the guard
// deduplicates on. Collapsing them would make an injected duplicate look like a
// sequence regression and make a redelivery look like an injected duplicate.
type envelope struct {
	// EventID is first on the wire so a reader that only needs the identity can
	// stop early; the consumer decodes the whole struct once and republishes the
	// id into the message metadata for the guard's KeyFunc.
	EventID string     `json:"eid"`
	Pub     string     `json:"pub"`
	Lane    int        `json:"lane"`
	Seq     uint64     `json:"seq"`
	Class   eventClass `json:"cls"`
	SentAt  int64      `json:"ts"`
	Pad     string     `json:"pad"`
}

// eventID is the logical identity. It is built from the publisher, the lane and
// the sequence of the FIRST copy, so the delayed re-publish of a treatment event
// reuses it verbatim while carrying its own wire sequence.
func eventID(publisher string, lane int, seq uint64) string {
	var b strings.Builder
	b.Grow(len(publisher) + 24)
	b.WriteString(publisher)
	b.WriteByte('-')
	b.WriteString(strconv.Itoa(lane))
	b.WriteByte('-')
	b.WriteString(strconv.FormatUint(seq, 10))
	return b.String()
}

// laneKey identifies one ordered wire stream. Sequence accounting is per key,
// never fleet-wide: two publisher replicas interleave on the stream and a single
// global counter would report every interleave as a gap.
func laneKey(publisher string, lane int) string {
	return publisher + "#" + strconv.Itoa(lane)
}

// padFor sizes the filler so a marshalled envelope lands at target bytes. The
// overhead is measured rather than assumed, because the JSON length of the
// numeric fields moves with the run (a sequence past 10^6 is longer than one at
// 10^1) and a hard-coded constant would silently drift the payload size the
// whole measurement is reported against.
func padFor(target int) string {
	if target <= 0 {
		return ""
	}
	probe := envelope{
		EventID: eventID("stress-publisher-00", 99, 1<<40),
		Pub:     "stress-publisher-00",
		Lane:    99,
		Seq:     1 << 40,
		Class:   classTreatment,
		SentAt:  1 << 40,
	}
	body, err := sonic.ConfigFastest.Marshal(&probe)
	if err != nil || len(body) >= target {
		return ""
	}
	return strings.Repeat("x", target-len(body))
}
