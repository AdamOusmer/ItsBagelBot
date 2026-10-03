// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func signalClosed(signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	default:
		return false
	}
}

func TestMessageResolutionIsIdempotentAndExclusive(t *testing.T) {
	ack, nack := (*Message).Ack, (*Message).Nack
	for _, tc := range []struct {
		name      string
		message   func() *Message
		win, lose func(*Message) bool
		wantAcked bool
	}{
		{"a new message acks and ignores a later nack", func() *Message { return NewMessage("id", nil) }, ack, nack, true},
		{"a new message nacks and ignores a later ack", func() *Message { return NewMessage("id", nil) }, nack, ack, false},
		{"a zero-value message acks", func() *Message { return &Message{} }, ack, nack, true},
		{"a zero-value message nacks", func() *Message { return &Message{} }, nack, ack, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.message()

			first, repeat, loser := tc.win(msg), tc.win(msg), tc.lose(msg)

			assert.Equal(t, []bool{true, true, false}, []bool{first, repeat, loser})
			assert.Equal(t, tc.wantAcked, signalClosed(msg.Acked()))
			assert.Equal(t, !tc.wantAcked, signalClosed(msg.Nacked()))
		})
	}
}
