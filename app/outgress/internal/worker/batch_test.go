package worker

import (
	"errors"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errAmbiguousTwitchFailure = errors.New("ambiguous twitch failure")

type batchAttempt struct {
	next     int
	executed []string
	failOn   string
}

func (a *batchAttempt) save(next int) error {
	a.next = next
	return nil
}

func (a *batchAttempt) execute(item outgress.Message) error {
	a.executed = append(a.executed, item.Type)
	if item.Type == a.failOn {
		return errAmbiguousTwitchFailure
	}
	return nil
}

func TestBatchRetryNeverRepeatsClaimedItems(t *testing.T) {
	items := []outgress.Message{{Type: "one"}, {Type: "two"}, {Type: "three"}}
	first := &batchAttempt{failOn: "two"}
	err := runBatchItems(items, 0, first.save, first.execute)
	require.ErrorIs(t, err, errAmbiguousTwitchFailure)
	assert.Equal(t, 2, first.next)
	assert.Equal(t, []string{"one", "two"}, first.executed)

	retry := &batchAttempt{next: first.next}
	require.NoError(t, runBatchItems(items, retry.next, retry.save, retry.execute))
	assert.Equal(t, []string{"three"}, retry.executed)
}

func TestBatchCheckpointFailureDoesNotSendItem(t *testing.T) {
	want := errors.New("valkey unavailable")
	executed := false
	err := runBatchItems([]outgress.Message{{Type: "chat"}}, 0,
		func(int) error { return want },
		func(outgress.Message) error { executed = true; return nil },
	)
	if !errors.Is(err, want) {
		t.Fatalf("run error = %v, want %v", err, want)
	}
	if executed {
		t.Fatal("item executed without an at-most-once checkpoint")
	}
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

func TestBatchJSONDecoderPrecompiles(t *testing.T) {
	if err := PrepareJSON(); err != nil {
		t.Fatal(err)
	}
}

// Acquire takes the lock, then Next reads back the cursor SaveNext wrote on the
// previous pass. A node-local replica that has not yet received that write
// hands the lock holder an older cursor and the batch resends chat lines it
// already delivered, so the store's reads must be primary-consistent.
func TestNewValkeyBatchStorePinsProgressReadsToThePrimary(t *testing.T) {
	assert.True(t, pkg_valkey.IsPrimary(NewValkeyBatchStore(nil).client),
		"batch progress is read back by the lock holder that wrote it")
}
