// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/kvstate/kvtest"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAmbiguousTwitchFailure = errors.New("ambiguous twitch failure")

const threeChatBatch = `{"id":"batch-1","items":[` +
	`{"type":"chat","payload":{"message":"one"}},` +
	`{"type":"chat","payload":{"message":"two"}},` +
	`{"type":"chat","payload":{"message":"three"}}]}`

func sentMessages(requests []recordedRequest) []string {
	var bodies []string
	for _, r := range requests {
		bodies = append(bodies, r.Body)
	}
	return bodies
}

func chatBody(text string) string { return `{"message":"` + text + `","sender_id":"` + testBot + `"}` }

type failingCheckpoints struct{ err error }

func (f failingCheckpoints) Acquire(context.Context, BatchLease, time.Duration) (bool, error) {
	return true, nil
}
func (f failingCheckpoints) Next(context.Context, string) (int, error) { return 0, nil }
func (f failingCheckpoints) SaveNext(context.Context, BatchLease, int, time.Duration) error {
	return f.err
}
func (f failingCheckpoints) Release(context.Context, BatchLease) error { return nil }

func TestBatchRetryNeverRepeatsClaimedItems(t *testing.T) {
	rt := &scriptedTransport{responses: []scriptedResponse{{status: 204}, {err: errAmbiguousTwitchFailure}}}
	w := pipelineWorker(t, rt, withBatchStore(NewJetStreamBatchStore(kvtest.New())))
	batch := testMessage{Type: "batch", Payload: threeChatBatch}

	require.ErrorIs(t, batch.send(w), errAmbiguousTwitchFailure)
	assert.Equal(t, []string{chatBody("one"), chatBody("two")}, sentMessages(rt.recorded()), "items go out in order, the failed one is claimed")

	require.NoError(t, batch.send(w))
	assert.Equal(t, []string{chatBody("one"), chatBody("two"), chatBody("three")}, sentMessages(rt.recorded()), "the retry sends only what was never claimed")
}

func TestBatchCheckpointFailureDoesNotSendItem(t *testing.T) {
	want := errors.New("valkey unavailable")
	rt := &scriptedTransport{}
	w := pipelineWorker(t, rt, withBatchStore(failingCheckpoints{err: want}))

	err := testMessage{Type: "batch", Payload: threeChatBatch}.send(w)

	require.ErrorIs(t, err, want)
	assert.Empty(t, rt.recorded(), "an item is never sent without its at-most-once checkpoint")
}

func TestBatchWithoutAStoreIsRetriedLater(t *testing.T) {
	rt := &scriptedTransport{}

	err := testMessage{Type: "batch", Payload: threeChatBatch}.send(pipelineWorker(t, rt))

	require.Error(t, err)
	assert.Empty(t, rt.recorded())
}

func TestBatchJSONDecoderPrecompiles(t *testing.T) {
	require.NoError(t, PrepareJSON())
}

func TestBatchWireCodecPreservesItems(t *testing.T) {
	body := []byte(`{"type":"batch","broadcaster_id":"123","payload":{"id":"batch-1","items":[{"type":"chat","payload":{"message":"one"}},{"type":"chat","payload":{"message":"two"}}]}}`)
	var got outgress.Message
	if err := decodeMessage(body, &got); err != nil {
		t.Fatal(err)
	}
	var batch outgress.Batch
	if err := decodeBatch(got.Payload, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.ID != "batch-1" || len(batch.Items) != 2 {
		t.Fatalf("decoded batch = %#v", batch)
	}
}

func TestNewValkeyBatchStorePinsProgressReadsToThePrimary(t *testing.T) {
	assert.True(t, pkg_valkey.IsPrimary(NewValkeyBatchStore(nil).client),
		"batch progress is read back by the lock holder that wrote it")
}
