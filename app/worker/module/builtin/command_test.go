package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func cmdCtx(text string, badges ...lane.Badge) *module.Context {
	return &module.Context{
		Env: lane.Envelope{
			Type:              "channel.chat.message",
			BroadcasterUserID: "2",
			ChatterUserID:     "1",
			Text:              text,
			Badges:            badges,
		},
		Regress:       module.RegressPremium,
		BroadcasterID: 2,
		Log:           zap.NewNop(),
	}
}

func run(t *testing.T, r projection.Reader, live module.LiveStore, cd module.CooldownStore, c *module.Context) []*replyView {
	t.Helper()
	m := NewCommandModule(r, live, cd, zap.NewNop())
	out, err := m.Handle(context.Background(), c)
	require.NoError(t, err)
	return decodeReplies(t, out)
}

func TestCustomCommandReplies(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "points", Response: "you have points", IsActive: true}}
	replies := run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!points"))
	require.Len(t, replies, 1)
	assert.Equal(t, "you have points", replies[0].Message)
	assert.Equal(t, "2", replies[0].BroadcasterID)
}

func TestInactiveCustomCommandIgnored(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "points", Response: "x", IsActive: false}}
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!points")))
}

func TestNonCommandIgnored(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "points", Response: "x", IsActive: true}}
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("just chatting")))
}

func TestLiveOnlyCommandSuppressedWhenOffline(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "uptime", Response: "live", IsActive: true, StreamOnlineOnly: true}}
	assert.Empty(t, run(t, r, &fakeLive{live: false}, &fakeCooldown{allow: true}, cmdCtx("!uptime")))
}

func TestLiveOnlyCommandRunsWhenLive(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "uptime", Response: "live", IsActive: true, StreamOnlineOnly: true}}
	replies := run(t, r, &fakeLive{live: true}, &fakeCooldown{allow: true}, cmdCtx("!uptime"))
	require.Len(t, replies, 1)
}

func TestPermDeniesBelowTier(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "ban", Response: "bye", IsActive: true, Perm: "mod"}}
	// everyone: denied
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!ban")))
	// moderator badge: allowed
	replies := run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!ban", lane.Badge{SetID: "moderator"}))
	require.Len(t, replies, 1)
}

func TestAllowedUserIDOverride(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "secret", Response: "hi", IsActive: true, Perm: "broadcaster", AllowedUserID: 1}}
	// chatter id "1" matches AllowedUserID, overrides the broadcaster perm tier.
	replies := run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!secret"))
	require.Len(t, replies, 1)

	// a different chatter is denied
	c := cmdCtx("!secret")
	c.Env.ChatterUserID = "999"
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, c))
}

func TestCooldownBlocks(t *testing.T) {
	r := fakeReader{found: true, command: projection.Command{Name: "spam", Response: "x", IsActive: true, Cooldown: 5}}
	cd := &fakeCooldown{allow: false}
	assert.Empty(t, run(t, r, &fakeLive{}, cd, cmdCtx("!spam")))
	assert.Equal(t, 1, cd.calls)
}

func TestDefaultCommandRuns(t *testing.T) {
	// "bot" is a shipped default; no custom command, no override row.
	r := fakeReader{found: false}
	replies := run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!bot"))
	require.Len(t, replies, 1)
	assert.Equal(t, defaultCommands["bot"].Response, replies[0].Message)
}

func TestDefaultCommandDisabledByOverride(t *testing.T) {
	r := fakeReader{found: false, modules: []projection.ModuleView{{Name: "command.bot", IsEnabled: false}}}
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!bot")))
}

func TestDefaultCommandResponseOverride(t *testing.T) {
	cfg, _ := json.Marshal(map[string]string{"response": "custom bagel reply"})
	r := fakeReader{found: false, modules: []projection.ModuleView{{Name: "command.bot", IsEnabled: true, Configs: cfg}}}
	replies := run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!bot"))
	require.Len(t, replies, 1)
	assert.Equal(t, "custom bagel reply", replies[0].Message)
}

func TestUnknownCommandIgnored(t *testing.T) {
	r := fakeReader{found: false}
	assert.Empty(t, run(t, r, &fakeLive{}, &fakeCooldown{allow: true}, cmdCtx("!doesnotexist")))
}
