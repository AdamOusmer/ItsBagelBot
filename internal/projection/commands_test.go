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

// Command-section round trips against the in-process fake Valkey
// (fakevalkey_test.go), beside the fetch section's fetch_test.go.

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

// The alias retirement read is the read half of a read-modify-write, and its
// result never gets a second chance: nothing revisits the row. A discarded
// read error reads as "no previous row", so no HDEL is emitted, the new body
// is committed anyway, and the removed aliases resolve forever.
func TestSetCommandFailsWhenTheAliasRetirementReadFails(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:77"

	require.NoError(t, store.SetCommand(ctx, commandDTO(77, "hello", "hi there", "hi", "yo")))
	require.Equal(t, "hello", f.hash(key)["cmdalias:hi"])
	require.Equal(t, "hello", f.hash(key)["cmdalias:yo"])
	before := f.hash(key)["command:hello"]

	// The row read fails while the rest of the server keeps working, so the
	// write that follows it would otherwise succeed on its own.
	f.failHGET("command:hello")
	err := store.SetCommand(ctx, commandDTO(77, "hello", "reworded, no aliases"))
	require.Error(t, err, "a failed retirement read must abort the write so the event is redelivered")

	f.failHGET("")
	h := f.hash(key)
	assert.Equal(t, before, h["command:hello"], "nothing may be committed from a read that failed")
	assert.Equal(t, "hello", h["cmdalias:hi"], "aliases stay whole; a half-retired set never converges")
	assert.Equal(t, "hello", h["cmdalias:yo"])
}

// A first write has no previous row at all: the Valkey nil is not a failure.
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

// Retirement itself still works: dropping an alias removes its pointer.
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

// A prior row that will not decode leaves the alias retirement uncomputable.
// Skipping it commits the new body and strands the old aliases exactly as a
// discarded read error did, and nothing revisits the row to notice. SetCommand
// never writes invalid JSON, so a row in this state was corrupted elsewhere
// and is worth surfacing rather than half-applying over.
func TestSetCommandFailsWhenThePriorRowDoesNotDecode(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:79"

	f.seed(key, fakeField{field: "command:hello", value: "{not json"})

	err := store.SetCommand(ctx, commandDTO(79, "hello", "hi there", "hi"))

	require.Error(t, err, "an undecodable prior row must fail the write, not be skipped")
	assert.NotEqual(t, "hello", f.hash(key)["cmdalias:hi"], "the new body must not commit over it")
}
