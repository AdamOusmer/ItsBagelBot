// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// chatterBotID is the bot's own Twitch id in these fixtures, wired into the
// pipeline exactly as the deployment wires TWITCH_BOT_USER_ID, so the
// exclusion is exercised through the same field production reads.
const chatterBotID = "555"

// chatterPipeline is one channel whose custom command names the chatter
// tokens, with the roster seeded by hand: the tokens read the roster this
// replica has observed, so a test seeds it the way a chat line would.
func chatterPipeline(t *testing.T, response string, speakers ...chatterIdentity) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  map[string]projection.ModuleView{},
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium:  premiumSubj,
		OutgressStandard: standardSubj,
		BotID:            chatterBotID,
	})
	for _, who := range speakers {
		p.roster.Observe(123, who)
	}
	return p
}

// spoke is one viewer the replica has watched talk in channel 123.
func spoke(id, login, name string) chatterIdentity {
	return chatterIdentity{id: id, login: login, name: name}
}

func TestChattersTokenCountsTheRoster(t *testing.T) {
	p := chatterPipeline(t, "{chatters} of us here",
		spoke("1", "sam", "Sam"), spoke("2", "alex", "Alex"))

	assert.Equal(t, "2 of us here", expandViewer(t, p, "!brag"))
}

// The pinned decision: a replica that has watched nobody speak answers "0",
// never a literal span. Nothing gates this scope, so the token is never the
// one that stays visible in chat.
func TestChattersTokenRendersZeroForAnUnknownRoster(t *testing.T) {
	p := chatterPipeline(t, "{chatters} of us here")
	assert.Equal(t, "0 of us here", expandViewer(t, p, "!brag"))
}

// The draw names a viewer, and names neither the bot nor the broadcaster: with
// both excluded only one drawable chatter is left, so the drawn name is fixed
// without pinning the dice.
func TestRandomChatterTokenExcludesTheBotAndTheBroadcaster(t *testing.T) {
	p := chatterPipeline(t, "say hi to {random.chatter}",
		spoke(chatterBotID, "bagelbot", "BagelBot"),
		spoke("123", "streamer", "Streamer"),
		spoke("42", "sam", "Sam"))

	assert.Equal(t, "say hi to Sam", expandViewer(t, p, "!brag"))
	assert.Equal(t, "3", expandViewer(t, chatterPipeline(t, "{chatters}",
		spoke(chatterBotID, "bagelbot", "BagelBot"),
		spoke("123", "streamer", "Streamer"),
		spoke("42", "sam", "Sam")), "!brag"), "the count is the whole room")
}

// A chatter the roster only ever learned a login for still renders: a
// folded-cohort sender carries no display name on the wire.
func TestRandomChatterTokenFallsBackToTheLogin(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter}", spoke("42", "sam", ""))
	assert.Equal(t, "hi sam", expandViewer(t, p, "!brag"))
}

// Roster names are viewer-supplied, so they are sanitized like {touser}: a
// display name that is a leading slash-verb cannot become one in the reply.
func TestRandomChatterTokenSanitizesTheDrawnName(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter}", spoke("42", "sam", "/me waves"))
	assert.Equal(t, "hi me waves", expandViewer(t, p, "!brag"))
}

// Nobody drawable resolves to empty, so the span's fallback speaks rather than
// the braces showing up in chat.
func TestRandomChatterTokenRendersItsFallbackWhenTheRoomIsEmpty(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter|everyone}")
	assert.Equal(t, "hi everyone", expandViewer(t, p, "!brag"))
}

// Neither token takes a payload, and a name neither scope owns keeps its
// braces: the unknown-token rule, pinned beside the tokens that answer.
func TestChatterTokensStayLiteralWhenMisspelled(t *testing.T) {
	p := chatterPipeline(t, "{chatters:5} {random.chatter:mods} {chatter}", spoke("42", "sam", "Sam"))
	assert.Equal(t, "{chatters:5} {random.chatter:mods} {chatter}", expandViewer(t, p, "!brag"))
}
