// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRPCUsesDeadlineFromDomainWireHeader(t *testing.T) {
	nc := testnats.Connect(t)
	subject := rpcSubjectName()
	require.NoError(t, Serve(RPCWiring{NC: nc, Queue: "deadline"}.Within(10*time.Second), subject,
		func(ctx context.Context, _ map[string]string) map[string]int64 {
			deadline, _ := ctx.Deadline()
			return map[string]int64{"deadline": deadline.UnixMilli()}
		}))
	deadline := time.Now().Add(2 * time.Second).UnixMilli()
	wire := nats.NewMsg(subject)
	wire.Data = []byte(`{}`)
	wire.Header.Set("Rpc-Deadline", strconv.FormatInt(deadline, 10))
	reply, err := nc.RequestMsg(wire, time.Second)
	require.NoError(t, err)

	var result map[string]int64
	require.NoError(t, codec.Unmarshal(reply.Data, &result))
	require.Equal(t, deadline, result["deadline"])
}

func TestLaneSubscriberUpdatesMetadataWithoutReplacingConsumer(t *testing.T) {
	f := newBusFixture(t)
	subject := "data.test.metadata." + nuid.Next()
	group := uniqueGroup("metadata")
	name := durableName(group, subject)
	existing := laneConsumerConfig(subject, group, name, 6)
	existing.Metadata = map[string]string{"previous.managed": "true"}
	existing.DeliverSubject = "_INBOX.previous." + nuid.Next()
	existing.DeliverPolicy = nats.DeliverLastPolicy
	before, err := f.js.AddConsumer(DataStream.Name, existing)
	require.NoError(t, err)

	sub, err := NewLaneSubscriber(LaneConfig{URL: f.url, Stream: DataStream.Name, Subject: subject,
		Group: group, NakDelay: time.Second, MaxRedeliveries: 5}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })

	after, err := f.js.ConsumerInfo(DataStream.Name, name)
	require.NoError(t, err)
	require.Equal(t, [2]string{"true", ""}, [2]string{after.Config.Metadata["bus.managed"], after.Config.Metadata["previous.managed"]})
	require.Equal(t, [3]any{before.Created, existing.DeliverSubject, existing.DeliverPolicy},
		[3]any{after.Created, after.Config.DeliverSubject, after.Config.DeliverPolicy})
}
