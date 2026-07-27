package main

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func TestShadowConsumerMatchesHotLaneReceiptSemantics(t *testing.T) {
	cfg := config{
		stream: "R3_SHADOW_BENCH", subject: "twitch.outgress.bench.r3.test.consumer",
		consumerName: "R3_SHADOW_CEILING", consumerGroup: "r3-shadow-ceiling",
		consumerMaxPending: 20_000,
	}
	consumer := shadowConsumerConfig(cfg)
	require.Equal(t, jsapi.AckFlowControlPolicy, consumer.AckPolicy)
	require.True(t, consumer.FlowControl)
	require.Equal(t, time.Second, consumer.IdleHeartbeat)
	require.Equal(t, 20_000, consumer.MaxAckPending)
	require.Equal(t, 0, consumer.Replicas)
	require.True(t, consumer.MemoryStorage)
	require.Equal(t, "_INBOX.BAGEL.R3.R3_SHADOW_CEILING", consumer.DeliverSubject)
	require.Equal(t, cfg.consumerGroup, consumer.DeliverGroup)
}

func TestShadowFlowControlResponseCarriesReceiptCursor(t *testing.T) {
	control := nats.NewMsg("_INBOX.BAGEL.R3.R3_SHADOW_CEILING")
	control.Reply = "$JS.FC.R3_SHADOW_BENCH.token"
	cursor := &consumerCursor{consumer: 20_000, stream: 22_000}

	response, sequence := shadowFlowControlResponse(control, cursor)
	require.NotNil(t, response)
	require.Equal(t, uint64(20_000), sequence)
	require.Equal(t, control.Reply, response.Subject)
	require.Equal(t, "20000", response.Header.Get(consumerLastSequenceHeader))
	require.Equal(t, "22000", response.Header.Get(streamLastSequenceHeader))
}

func TestShadowConsumerFinalStateRequiresZeroLag(t *testing.T) {
	state := consumerStateResult{
		DeliveredConsumer: 120_000,
		AckFloorConsumer:  120_000,
		Expected:          120_000,
	}
	require.True(t, consumerStateReady(state))
	state.AckPending = 1
	require.False(t, consumerStateReady(state))
	state.AckPending = 0
	state.Pending = 1
	require.False(t, consumerStateReady(state))
	state.Pending = 0
	// Nothing under AckFlowControl can redeliver, so the settle gate reports the
	// counter without asserting on it.
	state.Redelivered = 1
	require.True(t, consumerStateReady(state))
}

func TestShadowFlowControlAnswersBeforeAnyDelivery(t *testing.T) {
	control := nats.NewMsg("_INBOX.BAGEL.R3.R3_SHADOW_CEILING")
	control.Reply = "$JS.FC.R3_SHADOW_BENCH.token"

	// The window reopens on the response arriving, so an empty cursor must still
	// answer; it simply claims no ack floor, exactly like the production lane.
	response, sequence := shadowFlowControlResponse(control, &consumerCursor{})
	require.NotNil(t, response)
	require.Zero(t, sequence)
	require.Empty(t, response.Header.Get(streamLastSequenceHeader))
}

func TestShadowStalledHeartbeatClaimsOnlyTheReceiptCursor(t *testing.T) {
	heartbeat := nats.NewMsg("_INBOX.BAGEL.R3.R3_SHADOW_CEILING")
	heartbeat.Header.Set(consumerStatusHeader, "100")
	heartbeat.Header.Set(consumerStalledHeader, "$JS.FC.R3_SHADOW_BENCH.stalled")
	heartbeat.Header.Set(consumerLastSequenceHeader, "500")
	heartbeat.Header.Set(streamLastSequenceHeader, "900")

	response, sequence := shadowFlowControlResponse(heartbeat, &consumerCursor{consumer: 42, stream: 99})
	require.NotNil(t, response)
	require.Equal(t, "$JS.FC.R3_SHADOW_BENCH.stalled", response.Subject)
	// The heartbeat reports what the server sent. Answering with it would move
	// the replicated ack floor past deliveries this process never received.
	require.Equal(t, uint64(42), sequence)
	require.Equal(t, "42", response.Header.Get(consumerLastSequenceHeader))
	require.Equal(t, "99", response.Header.Get(streamLastSequenceHeader))
}

func TestTemporaryStreamStorageIsAMatrixAxis(t *testing.T) {
	cfg := config{stream: "R3_SHADOW_BENCH", subject: "twitch.outgress.bench.r3.test", replicas: 3}
	require.Equal(t, jsapi.MemoryStorage, temporaryStreamConfig(cfg).Storage)
	cfg.storage = "file"
	require.Equal(t, jsapi.FileStorage, temporaryStreamConfig(cfg).Storage)
}

func TestShadowConsumerModesAreSeparateRoles(t *testing.T) {
	cfg := config{
		consumerName: defaultShadowConsumer, consumerGroup: defaultShadowGroup,
		consumerMaxPending: 20_000, consumerSettleTimeout: time.Second,
	}
	cfg.consumerOnly = true
	require.Equal(t, "selected", boolLabel(cfg.consumerOnly))
	require.Equal(t, "", boolLabel(cfg.consumerCheck))
}

func TestShadowConsumerHonoursTheConfiguredAckWindow(t *testing.T) {
	cfg := config{
		stream: "R3_SHADOW_BENCH", subject: "twitch.outgress.bench.r3.test.consumer",
		consumerName: "R3_SHADOW_CEILING", consumerGroup: "r3-shadow-ceiling",
		consumerMaxPending: 5_000,
	}
	require.Equal(t, 5_000, shadowConsumerConfig(cfg).MaxAckPending)
}

func TestShadowConsumerNamesAreExact(t *testing.T) {
	for _, name := range []string{"R3_SHADOW_CEILING", "consumer-1"} {
		require.True(t, safeConsumerName(name))
	}
	for _, name := range []string{"", "consumer.*", "consumer name", "consumer.>"} {
		require.False(t, safeConsumerName(name))
	}
}
