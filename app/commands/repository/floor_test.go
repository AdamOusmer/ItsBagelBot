package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/commands/repository"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// floorSlur pulls a term from the embedded hate artifact so no slur sits in
// test source.
func floorSlur(t *testing.T) string {
	t.Helper()
	terms := moderation.EmbeddedLexicon().Terms(moderation.CatHate)
	require.NotEmpty(t, terms)
	return terms[0]
}

// A custom command whose response, name or alias carries immovable-floor
// content (hate, IP-grabber hosts) is refused at creation: the bot would post
// or echo that text as itself, risking the broadcaster's channel and the bot
// account platform-wide. This exercises Upsert - the exact path the dashboard
// create/edit RPC lands on - not just the validator in isolation.
func TestUpsertRefusesFloorContent(t *testing.T) {
	client, pub, repo := setup(t)
	ctx := context.Background()
	slur := floorSlur(t)

	cases := map[string]error{
		"response slur":       repo.Upsert(1001, spec("!hi", "welcome "+slur+" friends", false, 0)),
		"response ip-grabber": repo.Upsert(1001, spec("!pc", "specs at grabify.link/pc", false, 0)),
		"slur name":           repo.Upsert(1001, spec("!"+slur, "hello chat", false, 0)),
	}
	for label, err := range cases {
		assert.ErrorIs(t, err, validate.ErrContentFloor, label)
	}

	// A slur alias is refused too, with the precise floor error (not blurred
	// into the generic alias error).
	s := spec("!hi", "hello chat", false, 0)
	s.Aliases = []string{"!" + slur}
	assert.ErrorIs(t, repo.Upsert(1001, s), validate.ErrContentFloor, "slur alias")

	// An obfuscated slur folds onto the plain spelling and is refused the same.
	leet := ""
	for _, r := range slur {
		switch r {
		case 'a':
			r = '4'
		case 'e':
			r = '3'
		case 'i':
			r = '1'
		case 'o':
			r = '0'
		case 's':
			r = '5'
		}
		leet += string(r)
	}
	assert.ErrorIs(t, repo.Upsert(1001, spec("!x", "gg "+leet+" gg", false, 0)),
		validate.ErrContentFloor, "obfuscated slur response")

	// Nothing reached the store.
	repo.Close(ctx)
	assert.Equal(t, 0, client.Commands.Query().CountX(ctx))

	// Milder language saves fine: the floor is hate + abuse infrastructure
	// only, so profanity and giveaway phrasing are the broadcaster's call.
	repo2 := repository.NewCommands(client, pub, nil, zap.NewNop())
	require.NoError(t, repo2.Upsert(1001, spec("!gg", "damn that was some bullshit ref, hell of a game", false, 0)))
	require.NoError(t, repo2.Upsert(1001, spec("!prize", "type !prize to claim your prize tonight", false, 0)))
	repo2.Close(ctx)
	assert.Equal(t, 2, client.Commands.Query().CountX(ctx))
}
