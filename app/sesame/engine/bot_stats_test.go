// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package engine

import (
	"errors"
	"sync"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// bumpCall is one recorded CounterBumper call.
type bumpCall struct {
	name  string
	delta int64
}

// fakeBumper records the flusher's bumps. The ticker goroutine and Close can
// both drive it, so the slice takes a mutex.
type fakeBumper struct {
	mu  sync.Mutex
	got []bumpCall
}

func (b *fakeBumper) BumpBot(name string, delta int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, bumpCall{name: name, delta: delta})
}

func (b *fakeBumper) calls() []bumpCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bumpCall(nil), b.got...)
}

// statsPipeline builds a pipeline with the stats flusher armed, registered for
// cleanup so the goroutine never outlives the test.
func statsPipeline(t *testing.T, bumper CounterBumper) *Pipeline {
	t.Helper()
	d := Deps{
		Proj:     fakeReader{},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
		Stats:    bumper,
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	t.Cleanup(p.Close)
	return p
}

func eventMsg(t *testing.T, eventType string) *bus.Message {
	t.Helper()
	body, err := codec.Marshal(map[string]any{
		"type":                eventType,
		"lane":                "standard",
		"broadcaster_user_id": "123",
	})
	require.NoError(t, err)
	return bus.NewMessage("uuid-event", body)
}

func TestProcessCountsChatAsMessageAndEvent(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
	assert.Equal(t, int64(1), p.stats.events.Load())
	assert.Equal(t, int64(1), p.stats.messages.Load())
}

// A non-chat envelope with no registered handler is filtered by eligible(), and
// still counts as an event: the total is "everything sesame decoded".
func TestProcessCountsFilteredEventOnly(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(eventMsg(t, "stream.online")))
	assert.Equal(t, int64(1), p.stats.events.Load())
	assert.Zero(t, p.stats.messages.Load())
}

func TestProcessCountsNothingForMalformedEnvelope(t *testing.T) {
	p := statsPipeline(t, &fakeBumper{})

	require.NoError(t, p.Process(bus.NewMessage("uuid-bad", []byte("{not json"))))
	assert.Zero(t, p.stats.events.Load())
	assert.Zero(t, p.stats.messages.Load())
}

// A pipeline without a stats sink starts no flusher, and the hot path stays a
// no-op call rather than a nil dereference.
func TestProcessWithoutStatsSink(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, emitModule("", module.KindCore, "pong"))
	require.Nil(t, p.stats)
	require.NoError(t, p.Process(chatMsg(t, "standard", "hi")))
}

func TestBotStatsFlushBumpsAndResets(t *testing.T) {
	bumper := &fakeBumper{}
	s := newBotStats(bumper)
	s.count(true)
	s.count(true)
	s.count(false)
	s.Close() // flushes the remainder

	calls := bumper.calls()
	require.Len(t, calls, 2)
	byName := map[string]bumpCall{}
	for _, c := range calls {
		byName[c.name] = c
	}
	// The reserved-namespace shape (broadcaster 0, bot scope) is BumpBot's job,
	// proven against the real reporter in TestBotStatsBumpsPassReporterGuard.
	assert.Equal(t, int64(3), byName[counterEventsProcessed].delta)
	assert.Equal(t, int64(2), byName[counterMessagesProcessed].delta)

	// The swap reset both totals: an idle window bumps nothing.
	assert.Zero(t, s.events.Load())
	assert.Zero(t, s.messages.Load())
	s.flush()
	assert.Len(t, bumper.calls(), 2)
}

// The counting the hot path pays for must stay free: two atomics, no lock, no
// map, no allocation. Guards the same structural regression the pipeline's
// alloc ceiling does, one level down.
func TestBotStatsCountAllocFree(t *testing.T) {
	s := newBotStats(&fakeBumper{})
	t.Cleanup(s.Close)

	if avg := testing.AllocsPerRun(1000, func() { s.count(true) }); avg != 0 {
		t.Fatalf("count allocates %.1f allocs/op, must be 0", avg)
	}
}

// The reporter's own guard must accept what the flusher sends: broadcaster 0
// paired with bot scope is the only shape it lets through.
func TestBotStatsBumpsPassReporterGuard(t *testing.T) {
	pub := &rawPublisher{}
	r := NewLoyaltyReporter(pub, zap.NewNop())
	s := newBotStats(r)
	s.count(true)
	s.Close()
	r.Close()

	published := pub.payloads[data.SubjectLoyaltyCounters]
	require.Len(t, published, 1)
	var dto data.CounterBumpedDTO
	require.NoError(t, codec.Unmarshal(published[0], &dto))
	assert.Equal(t, uint64(0), dto.UserID)
	assert.Len(t, dto.Bumps, 2)
}

// The pipeline never counts a projection failure away: the nacked envelope
// still decoded, so it belongs to the lifetime total.
func TestProcessCountsEnvelopeBeforeModuleViews(t *testing.T) {
	d := Deps{
		Proj:     fakeReader{modErr: errors.New("projection down")},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
		Stats:    &fakeBumper{},
	}
	// A name-gated module is what makes the pipeline read the module views at
	// all, so the read failure can reach the ack decision.
	reg := NewRegistry(zap.NewNop(), emitModule("greeter", module.KindDefault, "pong"))
	p := NewPipeline(d, reg, Config{OutgressStandard: standardSubj})
	t.Cleanup(p.Close)

	assert.Error(t, p.Process(chatMsg(t, "standard", "hi")))
	assert.Equal(t, int64(1), p.stats.events.Load())
	assert.Equal(t, int64(1), p.stats.messages.Load())
}
