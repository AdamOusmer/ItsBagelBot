// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package events

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/internal/domain/rpc/deploy"
	"ItsBagelBot/pkg/codec"
)

type recordConn struct{ msgs []*nats.Msg }

func (c *recordConn) PublishMsg(m *nats.Msg) error {
	c.msgs = append(c.msgs, m)
	return nil
}

func TestPublishSendsFullSnapshotOnRunSubject(t *testing.T) {
	nc := &recordConn{}
	run := &deploy.Run{
		ID: "r1", Seq: 7, Kind: deploy.KindRelease, State: deploy.RunRunning,
		Stages: []deploy.Stage{{ID: deploy.StagePreflight, State: deploy.StateRunning}},
	}
	require.NoError(t, (&Publisher{nc: nc}).Publish(context.Background(), run))
	require.Len(t, nc.msgs, 1)

	var got deploy.Run
	require.NoError(t, codec.Unmarshal(nc.msgs[0].Data, &got))
	assert.Equal(t,
		[2]any{"bagel.deploy.events.r1", *run},
		[2]any{nc.msgs[0].Subject, got})
}
