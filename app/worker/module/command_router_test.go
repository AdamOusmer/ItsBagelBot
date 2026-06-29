package module

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// oneCommandReader returns the same custom command for any name, so a test can
// drive the router's custom-command path with a chosen response/permission.
type oneCommandReader struct{ cmd projection.Command }

func (r oneCommandReader) User(context.Context, uint64) (projection.User, error) {
	return projection.User{}, nil
}
func (r oneCommandReader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return nil, nil
}
func (r oneCommandReader) Command(context.Context, uint64, string) (projection.Command, bool, error) {
	return r.cmd, true, nil
}

type liveAlways struct{}

func (liveAlways) IsLive(context.Context, uint64) (bool, error) { return true, nil }
func (liveAlways) SetLive(context.Context, uint64) error        { return nil }
func (liveAlways) ClearLive(context.Context, uint64) error      { return nil }

func newRouter(resp, perm string) *CommandRouter {
	r := NewCommandRouter(
		oneCommandReader{cmd: projection.Command{Name: "so", Response: resp, IsActive: true, Perm: perm}},
		liveAlways{},
		NoopCooldown{},
		zap.NewNop(),
	)
	r.Bind(NewRegistry(zap.NewNop(), r))
	return r
}

func chatCtx(badgeRole string) *Context {
	env := lane.Envelope{
		Type:              "channel.chat.message",
		Text:              "!so @bob raid incoming",
		BroadcasterUserID: "123",
		ChatterUserID:     "999",
		ChatterUserLogin:  "alice",
	}
	if badgeRole != "" {
		env.Badges = []lane.Badge{{SetID: badgeRole}}
	}
	return &Context{Env: env, BroadcasterID: 123, Log: zap.NewNop()}
}

func collect(r *CommandRouter, c *Context) []Output {
	var got []Output
	_ = r.Handle(context.Background(), c, func(o *Output) { got = append(got, *o) })
	return got
}

// TestCustomAnnounceAllowedForEveryone asserts that a perm=everyone custom command
// whose response is "/announce ..." lets any viewer trigger an announce output.
func TestCustomAnnounceAllowedForEveryone(t *testing.T) {
	r := newRouter("/announce {user} says: {args}; target={target}", "everyone")

	// everyone chatter: announce goes through.
	got := collect(r, chatCtx(""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeAnnounce, got[0].Type)
	assert.Equal(t, "primary", got[0].Color)
	assert.Equal(t, "alice says: @bob raid incoming; target=bob", got[0].Text)
}

// TestCustomAnnounceEmptySkipped: a "/announce" with no text leaves an empty
// payload Twitch would reject, so the router emits nothing even for a moderator.
func TestCustomAnnounceEmptySkipped(t *testing.T) {
	r := newRouter("/announce", "everyone")
	assert.Empty(t, collect(r, chatCtx("moderator")))
}

// TestCustomPlainChatStillEmits: an ordinary custom response is unaffected by the
// new gates and emits a normal chat line for everyone.
func TestCustomPlainChatStillEmits(t *testing.T) {
	r := newRouter("hello {sender}", "everyone")
	got := collect(r, chatCtx(""))
	require.Len(t, got, 1)
	assert.Equal(t, outgress.TypeChat, got[0].Type)
}
