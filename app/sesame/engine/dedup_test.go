package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// recordingStore is an in-memory idempotency.Store that records every claimed
// key, so a test can assert which effects consulted the guard.
type recordingStore struct {
	mu     sync.Mutex
	claims map[string]bool
	seen   []string
}

func newRecordingStore() *recordingStore { return &recordingStore{claims: map[string]bool{}} }

func (r *recordingStore) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, key)
	if r.claims[key] {
		return true, nil
	}
	r.claims[key] = true
	return false, nil
}

func (r *recordingStore) Release(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.claims, key)
	return nil
}

func (r *recordingStore) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

// usedCount reads the single summed command-use publish (rawPublisher is
// defined in loyalty_test.go and shared across the engine test package).
func usedCount(t *testing.T, pub *rawPublisher) uint64 {
	t.Helper()
	msgs := pub.payloads[data.SubjectCommandUsed]
	require.Len(t, msgs, 1, "expected exactly one summed command-use publish")
	var dto data.CommandUsedDTO
	require.NoError(t, codec.Unmarshal(msgs[0], &dto))
	return dto.Count
}

func commandMsg(t *testing.T, msgID, text string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                chatType,
		"lane":                "standard",
		"msg_id":              msgID,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                text,
	})
	require.NoError(t, err)
	return bus.NewMessage("uuid-"+msgID, body)
}

// TestGuardedHandlerDedupsReplay drives a custom command through the pipeline
// twice under the same message id. The reply is emitted each pass (it is not
// guarded), but the non-idempotent use-counter effect is claimed once, so the
// summed count is 1 rather than 2.
func TestGuardedHandlerDedupsReplay(t *testing.T) {
	store := newRecordingStore()
	pub := &rawPublisher{}
	reader := fakeReader{cmd: projection.Command{Name: "foo", Response: "hi", IsActive: true}, cmdFound: true}

	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(),
		Dedup: NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, CountUses: true,
	})

	require.NoError(t, p.Process(commandMsg(t, "m1", "!foo")))
	require.NoError(t, p.Process(commandMsg(t, "m1", "!foo"))) // replay: same msg_id
	p.Close()                                                  // flush the summed use counter

	require.Contains(t, store.keys(), "m1:"+effectUse, "the use-counter effect should consult the guard")
	require.Equal(t, uint64(1), usedCount(t, pub), "a replayed command must count once, not twice")
}

// TestFirehoseSkipsGuard confirms a plain-chat line that dispatches no command
// and emits nothing never touches the store: the guard is opt-in per effect, so
// the read-only firehose pays nothing.
func TestFirehoseSkipsGuard(t *testing.T) {
	store := newRecordingStore()
	pub := &rawPublisher{}

	d := Deps{
		Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: pub, Log: zap.NewNop(),
		Dedup: NewEventDedup(store, "sesame:seen:", time.Minute, zap.NewNop()),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj, CountUses: true,
	})
	defer p.Close()

	require.NoError(t, p.Process(commandMsg(t, "m2", "just chatting, not a command")))
	require.Empty(t, store.keys(), "a plain-chat firehose message must not consult the dedup store")
}
