// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

type ackPosition struct{ legacy, domained int }

var (
	sequencePosition = ackPosition{legacy: 5, domained: 7}
	storedAtPosition = ackPosition{legacy: 7, domained: 9}
)

func ackToken(reply string, position ackPosition) (int64, bool) {
	if !strings.HasPrefix(reply, "$JS.ACK.") {
		return 0, false
	}
	tokens := strings.Split(reply, ".")
	var index int
	switch {
	case len(tokens) == 9:
		index = position.legacy
	case len(tokens) >= 11:
		index = position.domained
	default:
		return 0, false
	}
	value, err := strconv.ParseInt(tokens[index], 10, 64)
	return value, err == nil && value > 0
}

func jetStreamSequence(wire *nats.Msg) uint64 {
	sequence, _ := ackToken(wire.Reply, sequencePosition)
	return uint64(sequence)
}

func jetStreamStoredAt(reply string) time.Time {
	ns, ok := ackToken(reply, storedAtPosition)
	if !ok {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
