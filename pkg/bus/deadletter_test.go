// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func deadLetterJetStream(t *testing.T) nats.JetStreamContext {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	require.NoError(t, err)
	for _, spec := range []StreamSpec{BagelDataStream, BagelDeadLetterStream, {Name: "OTHER", Subjects: []string{"other.>"}}} {
		_, err := js.AddStream(&nats.StreamConfig{Name: spec.Name, Subjects: spec.Subjects, Duplicates: spec.Duplicates})
		require.NoError(t, err)
	}
	return js
}

func fetchDelivery(t *testing.T, js nats.JetStreamContext, subject string, header nats.Header) *nats.Msg {
	t.Helper()
	msg := nats.NewMsg(subject)
	msg.Data = []byte(`{"batch_id":"b1"}`)
	msg.Header = header
	_, err := js.PublishMsg(msg)
	require.NoError(t, err)
	sub, err := js.PullSubscribe(subject, "")
	require.NoError(t, err)
	got, err := sub.Fetch(1, nats.MaxWait(2*time.Second))
	require.NoError(t, err)
	return got[0]
}

func deadLetters(t *testing.T, js nats.JetStreamContext) uint64 {
	t.Helper()
	info, err := js.StreamInfo(BagelDeadLetterStream.Name)
	require.NoError(t, err)
	return info.State.Msgs
}

func TestTerminatedDataMessageMovesToTheDeadLetterStream(t *testing.T) {
	js := deadLetterJetStream(t)
	header := nats.Header{}
	header.Set(nats.MsgIdHdr, "decoded:abc")
	header.Set(nats.ExpectedStreamHdr, BagelDataStream.Name)
	header.Set("Traceparent", "00-trace")
	delivery := fetchDelivery(t, js, "data.loyalty.counters", header)
	s := &concurrentDurableSubscriber{js: js, stream: BagelDataStream.Name, consumer: "loyalty", log: zap.NewNop()}

	s.terminate(delivery, deadLetterMaxDeliveries)
	s.terminate(delivery, deadLetterMaxDeliveries)

	require.Equal(t, uint64(1), deadLetters(t, js))
	letter, err := js.GetLastMsg(BagelDeadLetterStream.Name, "dlq.data.loyalty.counters")
	require.NoError(t, err)
	require.JSONEq(t, `{"batch_id":"b1"}`, string(letter.Data))
	require.Equal(t, "data.loyalty.counters", letter.Header.Get(DeadLetterSubjectHeader))
	require.Equal(t, "decoded:abc", letter.Header.Get(DeadLetterMsgIDHeader))
	require.Equal(t, "loyalty", letter.Header.Get(DeadLetterConsumerHeader))
	require.Equal(t, deadLetterMaxDeliveries, letter.Header.Get(DeadLetterReasonHeader))
	require.Equal(t, "1", letter.Header.Get(DeadLetterDeliveriesHeader))
	require.Equal(t, "00-trace", letter.Header.Get("Traceparent"))
	require.Empty(t, letter.Header.Get(nats.ExpectedStreamHdr))
	require.Equal(t, "dlq:BAGEL_DATA:1", letter.Header.Get(nats.MsgIdHdr))
}

func TestTerminatedMessageOutsideBagelDataIsNotDeadLettered(t *testing.T) {
	js := deadLetterJetStream(t)
	delivery := fetchDelivery(t, js, "other.thing", nil)
	s := &concurrentDurableSubscriber{js: js, stream: "OTHER", consumer: "c", log: zap.NewNop()}

	s.terminate(delivery, deadLetterMaxDeliveries)

	require.Zero(t, deadLetters(t, js))
}

func TestDeadLetterSubjectsStayOutsideTheDataStream(t *testing.T) {
	require.False(t, matchesAnySubject(deadLetterSubject("data.loyalty.counters"), BagelDataStream.Subjects))
	require.True(t, matchesAnySubject(deadLetterSubject("data.loyalty.counters"), BagelDeadLetterStream.Subjects))
}

func TestDataLanesRetryThreeTimesWithBackoffBeforeDeadLettering(t *testing.T) {
	delay := newBackoffRetryDelay(dataRetryBackoff)
	require.Equal(t, 5*time.Second, delay.WaitTime(1))
	require.Equal(t, 20*time.Second, delay.WaitTime(2))
	require.Equal(t, time.Minute, delay.WaitTime(3))
	require.Equal(t, terminateDelivery, delay.WaitTime(4))
}

func TestDataRetryScheduleFitsInsideTheDataStreamMaxAge(t *testing.T) {
	delay := newBackoffRetryDelay(dataRetryBackoff)
	worst := time.Duration(delay.max) * newConcurrentDurableSubscriber(concurrentSubscriberConfig{}).handlerDeadline
	for _, wait := range dataRetryBackoff {
		worst += wait
	}
	require.Less(t, worst, BagelDataStream.MaxAge)
}
