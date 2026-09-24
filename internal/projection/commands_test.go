// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func commandDTO(userID uint64, name, response string, aliases ...string) data.CommandChangedDTO {
	return data.CommandChangedDTO{
		UserID:   userID,
		Name:     name,
		Aliases:  aliases,
		Response: response,
		IsActive: true,
		Perm:     "everyone",
	}
}

func TestSetCommandFailsWhenTheAliasRetirementReadFails(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:77"

	require.NoError(t, store.SetCommand(ctx, commandDTO(77, "hello", "hi there", "hi", "yo")))
	require.Equal(t, "hello", f.hash(key)["cmdalias:hi"])
	require.Equal(t, "hello", f.hash(key)["cmdalias:yo"])
	before := f.hash(key)["command:hello"]

	f.failHGET("command:hello")
	err := store.SetCommand(ctx, commandDTO(77, "hello", "reworded, no aliases"))
	require.Error(t, err, "a failed retirement read must abort the write so the event is redelivered")

	f.failHGET("")
	h := f.hash(key)
	assert.Equal(t, before, h["command:hello"], "nothing may be committed from a read that failed")
	assert.Equal(t, "hello", h["cmdalias:hi"], "aliases stay whole; a half-retired set never converges")
	assert.Equal(t, "hello", h["cmdalias:yo"])
}

func TestSetCommandFirstWriteHasNoPreviousRow(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetCommand(ctx, commandDTO(78, "solo", "body", "s")))

	view, found, _, err := store.GetCommand(ctx, 78, "s")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "solo", view.Name)
	assert.Equal(t, "solo", f.hash("settings:78")["cmdalias:s"])
}

func TestSetCommandRetiresDroppedAliases(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:79"

	require.NoError(t, store.SetCommand(ctx, commandDTO(79, "ping", "pong", "p", "pi")))
	require.NoError(t, store.SetCommand(ctx, commandDTO(79, "ping", "pong", "p")))

	h := f.hash(key)
	assert.Equal(t, "ping", h["cmdalias:p"])
	assert.NotContains(t, h, "cmdalias:pi", "an alias the event no longer carries is retired")

	_, found, _, err := store.GetCommand(ctx, 79, "pi")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestSetCommandFailsWhenThePriorRowDoesNotDecode(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:79"

	f.seed(key, fakeField{field: "command:hello", value: "{not json"})

	err := store.SetCommand(ctx, commandDTO(79, "hello", "hi there", "hi"))

	require.Error(t, err, "an undecodable prior row must fail the write, not be skipped")
	assert.NotEqual(t, "hello", f.hash(key)["cmdalias:hi"], "the new body must not commit over it")
}

func TestSetCommandsWritesTheMarkerInTheSameHSETAsTheRows(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:91"

	f.seed(key, fakeField{field: "command:gone", value: `{"name":"gone"}`})
	f.seed(key, fakeField{field: "cmdalias:g", value: "gone"})

	require.NoError(t, store.SetCommands(ctx, 91, []CommandView{
		{Name: "Hello", Aliases: []string{"Hi"}, Response: "hi there"},
	}))

	hdel, hset := -1, -1
	for i, op := range f.ops() {
		switch op.cmd {
		case "HDEL":
			hdel = i
		case "HSET":
			require.Equal(t, -1, hset, "one HSET only: a second one could publish the marker without the rows")
			hset = i
		}
	}
	require.NotEqual(t, -1, hdel, "stale rows are cleared before the section is rewritten")
	require.NotEqual(t, -1, hset)
	assert.Less(t, hdel, hset, "the clear must precede the write, never follow it")

	written := f.ops()[hset].args
	assert.Subset(t, written, []string{
		key,
		commandsMarkerField, "1",
		"command:hello",
		"cmdalias:hi", "hello",
	}, "marker, body and alias pointer all ride the one HSET")

	h := f.hash(key)
	assert.NotContains(t, h, "command:gone")
	assert.NotContains(t, h, "cmdalias:g")
	assert.Equal(t, "1", h[commandsMarkerField])

	cmds, projected, err := store.GetCommands(ctx, 91)
	require.NoError(t, err)
	assert.True(t, projected)
	require.Len(t, cmds, 1)
	assert.Equal(t, "Hello", cmds[0].Name)
}
