// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type laneConsumerManagerSpy struct {
	nats.JetStreamManager
	info      *nats.ConsumerInfo
	updateErr error
	updated   *nats.ConsumerConfig
	deleted   int
	added     *nats.ConsumerConfig
}

func (s *laneConsumerManagerSpy) ConsumerInfo(string, string, ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	return s.info, nil
}

func (s *laneConsumerManagerSpy) UpdateConsumer(_ string, config *nats.ConsumerConfig, _ ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	copy := *config
	s.updated = &copy
	return nil, s.updateErr
}

func (s *laneConsumerManagerSpy) DeleteConsumer(string, string, ...nats.JSOpt) error {
	s.deleted++
	return nil
}

func (s *laneConsumerManagerSpy) AddConsumer(_ string, config *nats.ConsumerConfig, _ ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	copy := *config
	s.added = &copy
	return &nats.ConsumerInfo{Config: copy}, nil
}

func TestEnsureConsumerDoesNotReplaceAfterTransientUpdateFailure(t *testing.T) {
	spy := &laneConsumerManagerSpy{
		info:      &nats.ConsumerInfo{Config: nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.existing"}},
		updateErr: context.DeadlineExceeded,
	}

	err := ensureConsumer(spy, "LANE", &nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.desired"})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Zero(t, spy.deleted, "a transient update failure must not replace the live durable")
	assert.Nil(t, spy.added, "a transient update failure must not recreate the durable")
}

func TestEnsureConsumerReplacesRecognizedImmutableTransitionAtAckFloor(t *testing.T) {
	spy := &laneConsumerManagerSpy{
		info: &nats.ConsumerInfo{
			Config: nats.ConsumerConfig{
				Name:           "worker",
				DeliverSubject: "_INBOX.existing",
				DeliverPolicy:  nats.DeliverByStartSequencePolicy,
				OptStartSeq:    17,
			},
			AckFloor: nats.SequenceInfo{Stream: 41},
		},
		updateErr: errors.New("nats: ack policy can not be updated"),
	}

	err := ensureConsumer(spy, "LANE", &nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.desired"})

	require.NoError(t, err)
	require.NotNil(t, spy.added, "an immutable transition must be replaced exactly once")
	assert.Equal(t, 1, spy.deleted)
	assert.Equal(t, [2]any{"_INBOX.existing", uint64(17)}, [2]any{spy.updated.DeliverSubject, spy.updated.OptStartSeq},
		"the update must not lose the legacy binding or start position")
	assert.Equal(t, [3]any{"_INBOX.existing", nats.DeliverByStartSequencePolicy, uint64(42)},
		[3]any{spy.added.DeliverSubject, spy.added.DeliverPolicy, spy.added.OptStartSeq},
		"the replacement must keep the binding and resume at the ack floor + 1")
}

func TestReplaceConsumerCarriesAckFloor(t *testing.T) {
	for _, tc := range []struct {
		name         string
		ackFloor     uint64
		wantPolicy   nats.DeliverPolicy
		wantStartSeq uint64
	}{
		{"a known ack floor resumes one past it", 41, nats.DeliverByStartSequencePolicy, 42},
		{"a zero ack floor keeps the original delivery policy", 0, nats.DeliverAllPolicy, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := laneConsumerConfig(hotLane, "worker", "worker_twitch_ingress_event_premium", 6)

			carryAckFloor(desired, &nats.ConsumerInfo{AckFloor: nats.SequenceInfo{Stream: tc.ackFloor}})

			assert.Equal(t, tc.wantPolicy, desired.DeliverPolicy)
			assert.Equal(t, tc.wantStartSeq, desired.OptStartSeq)
		})
	}
}

func TestFleetSubscriberHasBoundedPacedRedelivery(t *testing.T) {
	assert.True(t, fleetMaxRedeliveries > 0 && fleetMaxRedeliveries <= 10, "fleet redeliveries = %d, want a small bounded budget", fleetMaxRedeliveries)
	assert.Positive(t, fleetNakDelay, "redelivery must be paced")
}
