// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const chatterBotID = "555"

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

func spoke(id, login, name string) chatterIdentity {
	return chatterIdentity{id: id, login: login, name: name}
}

func TestChattersTokenCountsTheRoster(t *testing.T) {
	p := chatterPipeline(t, "{chatters} of us here",
		spoke("1", "sam", "Sam"), spoke("2", "alex", "Alex"))

	assert.Equal(t, "2 of us here", expandViewer(t, p, "!brag"))
}

func TestChattersTokenRendersZeroForAnUnknownRoster(t *testing.T) {
	p := chatterPipeline(t, "{chatters} of us here")
	assert.Equal(t, "0 of us here", expandViewer(t, p, "!brag"))
}

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

func TestRandomChatterTokenFallsBackToTheLogin(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter}", spoke("42", "sam", ""))
	assert.Equal(t, "hi sam", expandViewer(t, p, "!brag"))
}

func TestRandomChatterTokenSanitizesTheDrawnName(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter}", spoke("42", "sam", "/me waves"))
	assert.Equal(t, "hi me waves", expandViewer(t, p, "!brag"))
}

func TestRandomChatterTokenRendersItsFallbackWhenTheRoomIsEmpty(t *testing.T) {
	p := chatterPipeline(t, "hi {random.chatter|everyone}")
	assert.Equal(t, "hi everyone", expandViewer(t, p, "!brag"))
}

func TestChatterTokensStayLiteralWhenMisspelled(t *testing.T) {
	p := chatterPipeline(t, "{chatters:5} {random.chatter:mods} {chatter}", spoke("42", "sam", "Sam"))
	assert.Equal(t, "{chatters:5} {random.chatter:mods} {chatter}", expandViewer(t, p, "!brag"))
}
