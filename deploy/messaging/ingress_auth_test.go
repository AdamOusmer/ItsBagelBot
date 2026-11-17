// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIngressConsumersCanQuarantine(t *testing.T) {
	blocks := busUserBlocks(t)
	for _, name := range []string{"worker_bus", "projector_bus", "outgress_bus"} {
		assert.True(t, blocks[name].grants("ingress.dlq.>"), name)
	}
}

func TestHotIngressOwnershipOnBroker(t *testing.T) {
	server := newLiveACLServer(t, committedACL(t))
	for _, subject := range []string{
		"$JS.API.STREAM.CREATE.TWITCH_INGRESS_PREMIUM",
		"$JS.API.STREAM.UPDATE.TWITCH_INGRESS_STANDARD",
		"$JS.API.STREAM.UPDATE.TWITCH_INGRESS",
		"$JS.API.CONSUMER.CREATE.TWITCH_INGRESS_PREMIUM.test",
		"$JS.API.CONSUMER.PAUSE.TWITCH_INGRESS.test",
		"$JS.API.CONSUMER.PAUSE.TWITCH_INGRESS_PREMIUM.test",
		"$JS.API.CONSUMER.PAUSE.TWITCH_INGRESS_STANDARD.test",
	} {
		err := server.publishOutcome(t, roleEndpoint{"BUS", "projector_bus"}, subject, 100*time.Millisecond)
		assert.ErrorContains(t, err, "Permissions Violation", subject)
		assert.NoError(t, server.publishOutcome(t, roleEndpoint{"BUS", "worker_bus"}, subject, 50*time.Millisecond), subject)
	}
}
