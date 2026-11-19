// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfirmedFuturesPreserveAdmissionOrderAndIdentity(t *testing.T) {
	f := newBusFixture(t)
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	pub := openPublisher(t, f.url)
	futures := make([]PublishFuture, 64)
	for i := range futures {
		future, err := BeginConfirmed(t.Context(), pub, Publication{Subject: publishBurstSubject,
			ID: fmt.Sprintf("ordered-%d", i), Payload: []byte(fmt.Sprint(i))})
		require.NoError(t, err)
		futures[i] = future
	}
	for _, future := range futures {
		require.NoError(t, future.Await(t.Context()))
	}
	info, err := f.js.StreamInfo(DataStream.Name)
	require.NoError(t, err)
	for i := range futures {
		msg, err := f.js.GetMsg(DataStream.Name, info.State.FirstSeq+uint64(i))
		require.NoError(t, err)
		require.Equal(t, []string{fmt.Sprint(i), fmt.Sprintf("ordered-%d", i)}, []string{string(msg.Data), msg.Header.Get(MessageIDHeader)})
	}
}
