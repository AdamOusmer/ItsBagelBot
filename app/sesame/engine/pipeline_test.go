package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- shared test doubles (used across the engine test files) ---

// captured is one published outgress message with the subject it rode.
type captured struct {
	subject string
	msg     outgress.Message
}

type fakePublisher struct {
	got     []captured
	failErr error
}

func (p *fakePublisher) Publish(subject string, msgs ...*message.Message) error {
	if p.failErr != nil {
		return p.failErr
	}
	for _, m := range msgs {
		var om outgress.Message
		_ = sonic.Unmarshal(m.Payload, &om)
		p.got = append(p.got, captured{subject: subject, msg: om})
	}
	return nil
}

func (p *fakePublisher) Close() error { return nil }

// fakeReader is a configurable projection.Reader.
type fakeReader struct {
	user     projection.User
	modules  []projection.ModuleView
	modErr   error
	cmd      projection.Command
	cmdFound bool
}

func (r fakeReader) User(context.Context, uint64) (projection.User, error) { return r.user, nil }
func (r fakeReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return r.modules, r.modErr
}
func (r fakeReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return r.cmd, r.cmdFound, nil
}

// liveAlways is a LiveStore that reports live and no-ops its writes.
type liveAlways struct{}

func (liveAlways) IsLive(context.Context, uint64) (bool, error) { return true, nil }
func (liveAlways) SetLive(context.Context, uint64) error        { return nil }
func (liveAlways) ClearLive(context.Context, uint64) error      { return nil }

const (
	premiumSubj  = "outgress.premium"
	standardSubj = "outgress.standard"
)

func newPipelineWith(pub message.Publisher, reader projection.Reader, mods ...module.Module) *Pipeline {
	reg := NewRegistry(zap.NewNop(), mods...)
	d := Deps{Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.NewNop()}
	return NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
}

func chatMsg(t *testing.T, laneName, text string) *message.Message {
	t.Helper()
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
		"lane":                laneName,
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                text,
	})
	require.NoError(t, err)
	return message.NewMessage("uuid-1", body)
}

// bareModule is a module with only a Kind/Name, for enabled() unit tests.
func bareModule(name string, kind module.Kind) module.Module {
	return module.NewModule(name, kind).Build()
}

// emitModule emits one fixed chat line on the chat event path. It fills a pooled
// Output like a real handler.
func emitModule(name string, kind module.Kind, text string) module.Module {
	b := module.NewModule(name, kind)
	b.On(chatType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		o := GetOutput()
		defer PutOutput(o)
		o.Type = outgress.TypeChat
		o.BroadcasterID = c.Env.BroadcasterUserID
		o.Text = text
		emit(o)
		return nil
	})
	return b.Build()
}

func emitLocaleModule(eventType string) module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(eventType, func(_ context.Context, c *module.Context, emit module.Emit) error {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          c.Locale,
		})
		return nil
	})
	return b.Build()
}

// errCore is a core module whose chat handler returns a logic error.
func errCore() module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error {
		return errors.New("boom")
	})
	return b.Build()
}

// === enabled() gate tests ===

func TestEnabledCoreModuleAlwaysRuns(t *testing.T) {
	p := &Pipeline{}
	mctx := &module.Context{Config: []byte("stale")}
	assert.True(t, p.enabled(bareModule("", module.KindCore), nil, mctx))
	assert.Nil(t, mctx.Config) // core modules carry no config
}

func TestEnabledDefaultModule(t *testing.T) {
	p := &Pipeline{}
	m := bareModule("feature", module.KindDefault)

	// row present, enabled, with config -> runs and config is wired in
	views := map[string]projection.ModuleView{"feature": {Name: "feature", IsEnabled: true, Configs: []byte(`{"x":1}`)}}
	mctx := &module.Context{}
	assert.True(t, p.enabled(m, views, mctx))
	assert.Equal(t, []byte(`{"x":1}`), []byte(mctx.Config))

	// row present, disabled -> skipped
	off := map[string]projection.ModuleView{"feature": {Name: "feature", IsEnabled: false}}
	assert.False(t, p.enabled(m, off, &module.Context{}))

	// no row -> ships enabled (default on)
	assert.True(t, p.enabled(m, nil, &module.Context{}))
}

func TestEnabledOptInModule(t *testing.T) {
	p := &Pipeline{}
	m := bareModule("shoutout", module.KindOptIn)

	// no row -> off
	assert.False(t, p.enabled(m, nil, &module.Context{}))

	// row enabled -> on, config wired
	on := map[string]projection.ModuleView{"shoutout": {Name: "shoutout", IsEnabled: true, Configs: []byte(`{"m":"hi"}`)}}
	mctx := &module.Context{}
	assert.True(t, p.enabled(m, on, mctx))
	assert.Equal(t, []byte(`{"m":"hi"}`), []byte(mctx.Config))

	// row disabled -> off
	off := map[string]projection.ModuleView{"shoutout": {Name: "shoutout", IsEnabled: false}}
	assert.False(t, p.enabled(m, off, &module.Context{}))
}

// === Process() tests ===

func TestProcessMalformedEnvelopeDropped(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "x"))
	err := p.Process(message.NewMessage("uuid-bad", []byte("{not json")))
	assert.NoError(t, err)   // ack, not nack
	assert.Empty(t, pub.got) // nothing published
}

func TestProcessLoadsLocaleForEventHandlers(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{user: projection.User{Locale: "fr"}}, emitLocaleModule("stream.online"))
	body, err := sonic.Marshal(map[string]any{
		"type":                "stream.online",
		"lane":                "standard",
		"broadcaster_user_id": "123",
	})
	require.NoError(t, err)

	require.NoError(t, p.Process(message.NewMessage("uuid-locale", body)))
	require.Len(t, pub.got, 1)
	assert.Equal(t, "fr", chatMessageText(t, pub.got[0].msg))
}

func TestProcessNoModuleAcks(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}) // empty registry
	err := p.Process(chatMsg(t, "premium", "hi"))
	assert.NoError(t, err)
	assert.Empty(t, pub.got)
}

func TestProcessChatEmittedToStandardLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, standardSubj, pub.got[0].subject)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
	assert.Equal(t, "123", pub.got[0].msg.BroadcasterID)

	var inner struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}
	require.NoError(t, sonic.Unmarshal(pub.got[0].msg.Payload, &inner))
	assert.Equal(t, "pong", inner.Message)
	assert.Equal(t, "123", inner.BroadcasterID)
}

func TestProcessChatEmittedToPremiumLane(t *testing.T) {
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "premium", "hi"))
	require.NoError(t, err)
	require.Len(t, pub.got, 1)
	assert.Equal(t, premiumSubj, pub.got[0].subject)
}

func TestProcessModuleErrorSkippedNotNacked(t *testing.T) {
	pub := &fakePublisher{}
	// one failing module, one good module: error is logged+skipped, good still emits
	p := newPipelineWith(pub, fakeReader{}, errCore(), emitModule("", module.KindCore, "still here"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	assert.NoError(t, err) // logic error does NOT nack
	require.Len(t, pub.got, 1)
	assert.Equal(t, outgress.TypeChat, pub.got[0].msg.Type)
}

func TestProcessPublishErrorNacks(t *testing.T) {
	pub := &fakePublisher{failErr: errors.New("broker down")}
	p := newPipelineWith(pub, fakeReader{}, emitModule("", module.KindCore, "pong"))
	err := p.Process(chatMsg(t, "standard", "hi"))
	assert.Error(t, err) // publish failure DOES nack
}
