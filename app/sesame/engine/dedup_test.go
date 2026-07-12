package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeDedup is an in-memory DedupStore. Claim wins the first time a key is seen
// and folds every later delivery until it is Released. claimErr/relErr force the
// error paths.
type fakeDedup struct {
	mu       sync.Mutex
	held     map[string]bool
	released []string
	claimErr error
	relErr   error
}

func newFakeDedup() *fakeDedup { return &fakeDedup{held: map[string]bool{}} }

func (d *fakeDedup) Claim(_ context.Context, key string) (bool, error) {
	if d.claimErr != nil {
		return false, d.claimErr
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held[key] {
		return false, nil // replicated duplicate
	}
	d.held[key] = true
	return true, nil
}

func (d *fakeDedup) Release(_ context.Context, key string) error {
	if d.relErr != nil {
		return d.relErr
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.held, key)
	d.released = append(d.released, key)
	return nil
}

func newPipelineWithDedup(pub bus.Publisher, reader projection.Reader, dedup DedupStore, mods ...module.Module) *Pipeline {
	reg := NewRegistry(zap.NewNop(), mods...)
	d := Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Dedup: dedup, Pub: pub, Log: zap.NewNop()}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

// chatEvent is chatMsg with separate EventSub delivery and Twitch chat IDs.
func chatEvent(t *testing.T, laneName, text, eventID string) *message.Message {
	t.Helper()
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                laneName,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                text,
		"event_id":            eventID,
		"msg_id":              "chat-message-1",
	})
	require.NoError(t, err)
	return message.NewMessage("uuid-"+eventID, body)
}

func TestProcessFoldsReplicatedDelivery(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWithDedup(pub, fakeReader{}, newFakeDedup(), emitModule("", module.KindCore, "pong"))

	// Same EventSub delivery id delivered twice (a replicated ingress publish).
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))

	assert.Len(t, pub.got, 1) // second delivery folded, emitted once
}

func TestProcessDistinctEventIDsBothEmit(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWithDedup(pub, fakeReader{}, newFakeDedup(), emitModule("", module.KindCore, "pong"))

	// The chat message id is deliberately the same: dedup keys only event_id.
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-2")))

	assert.Len(t, pub.got, 2)
}

func TestProcessNoEventIDNotFolded(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWithDedup(pub, fakeReader{}, newFakeDedup(), emitModule("", module.KindCore, "pong"))

	// Envelopes without an EventSub id can't be deduped; both are processed.
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))

	assert.Len(t, pub.got, 2)
}

func TestProcessDedupClaimErrorFailsOpen(t *testing.T) {
	pub := &fakePublisher{}
	dd := newFakeDedup()
	dd.claimErr = errors.New("valkey down")
	p := newPipelineWithDedup(pub, fakeReader{}, dd, emitModule("", module.KindCore, "pong"))

	// Claim errors: fail open, process instead of dropping the event.
	require.NoError(t, p.Process(chatEvent(t, "standard", "hi", "event-1")))
	assert.Len(t, pub.got, 1)
}

func TestProcessPublishErrorReleasesDedup(t *testing.T) {
	pub := &fakePublisher{failErr: errors.New("broker down")}
	dd := newFakeDedup()
	p := newPipelineWithDedup(pub, fakeReader{}, dd, emitModule("", module.KindCore, "pong"))

	// Publish fails -> nack -> the claim must be released so the redelivery retries.
	err := p.Process(chatEvent(t, "standard", "hi", "event-1"))
	assert.Error(t, err)
	assert.Equal(t, "sesame:dedup:123:event-1", dd.released[0])
	assert.Empty(t, dd.held) // key no longer held
}

func TestProcessModuleViewsErrorReleasesDedup(t *testing.T) {
	pub := &fakePublisher{}
	dd := newFakeDedup()
	// A name-gated module on chat makes the pipeline fetch ModuleViews, which errors.
	reader := fakeReader{modErr: errors.New("projection down")}
	p := newPipelineWithDedup(pub, reader, dd, emitModule("feature", module.KindDefault, "pong"))

	err := p.Process(chatEvent(t, "standard", "hi", "event-1"))
	assert.Error(t, err)                                        // infra failure: nack
	assert.Equal(t, "sesame:dedup:123:event-1", dd.released[0]) // claim released
}
