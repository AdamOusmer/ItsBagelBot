// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/discord/ent/ticket"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/rpc/discorddata"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openOne is the common "member opens a ticket in a fresh channel" call.
func openOne(t *testing.T, repo *repository.Store, ctx context.Context, channelID string, limit int) (int, int, error) {
	t.Helper()
	return repo.TicketOpen(ctx, repository.OpenParams{
		GuildID:   "g1",
		ChannelID: channelID,
		OpenerID:  "member1",
		Subject:   "help me",
		Limit:     limit,
	})
}

func TestTicketOpenRecordsAndCounts(t *testing.T) {
	repo, ctx := newStore(t, "ticketopen")

	id, count, err := openOne(t, repo, ctx, "c1", 3)
	require.NoError(t, err)
	assert.NotZero(t, id)
	assert.Equal(t, 1, count)

	row, found, err := repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ticket.StatusOpen, row.Status)
	assert.Equal(t, "member1", row.OpenerID)
	assert.Equal(t, "help me", row.Subject)
}

func TestTicketOpenRefusesPastTheLimit(t *testing.T) {
	repo, ctx := newStore(t, "ticketlimit")

	_, _, err := openOne(t, repo, ctx, "c1", 2)
	require.NoError(t, err)
	_, count, err := openOne(t, repo, ctx, "c2", 2)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	_, count, err = openOne(t, repo, ctx, "c3", 2)
	assert.ErrorIs(t, err, repository.ErrOpenLimit)
	assert.Equal(t, 2, count, "the refusal carries the count the caller names back")

	_, found, err := repo.TicketGet(ctx, "g1", "c3")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTicketOpenLimitZeroIsUnlimited(t *testing.T) {
	repo, ctx := newStore(t, "ticketnolimit")

	for i := range 4 {
		_, _, err := openOne(t, repo, ctx, "c"+strconv.Itoa(i), 0)
		require.NoError(t, err)
	}
	rows, _, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1"})
	require.NoError(t, err)
	assert.Len(t, rows, 4)
}

func TestTicketOpenIsIdempotentPerChannel(t *testing.T) {
	repo, ctx := newStore(t, "ticketidempotent")

	first, _, err := openOne(t, repo, ctx, "c1", 1)
	require.NoError(t, err)

	// A replay of the same channel returns the same row rather than tripping
	// the limit it is already counted against.
	second, _, err := openOne(t, repo, ctx, "c1", 1)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestTicketOpenClosedTicketsDoNotCountAgainstTheLimit(t *testing.T) {
	repo, ctx := newStore(t, "ticketlimitclosed")

	_, _, err := openOne(t, repo, ctx, "c1", 1)
	require.NoError(t, err)
	_, _, err = repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c1", ClosedBy: "staff1"})
	require.NoError(t, err)

	_, count, err := openOne(t, repo, ctx, "c2", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTicketClaimTransition(t *testing.T) {
	repo, ctx := newStore(t, "ticketclaim")

	id, _, err := openOne(t, repo, ctx, "c1", 1)
	require.NoError(t, err)

	claimed, err := repo.TicketClaim(ctx, "g1", "c1", "staff1")
	require.NoError(t, err)
	assert.Equal(t, id, claimed)

	row, _, err := repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusClaimed, row.Status)
	assert.Equal(t, "staff1", row.ClaimedBy)
	require.NotNil(t, row.ClaimedAt)
	firstClaim := *row.ClaimedAt

	// A hand-off keeps the original claimed_at so the desk's elapsed time
	// still measures from the first response.
	_, err = repo.TicketClaim(ctx, "g1", "c1", "staff2")
	require.NoError(t, err)
	row, _, err = repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	assert.Equal(t, "staff2", row.ClaimedBy)
	assert.WithinDuration(t, firstClaim, *row.ClaimedAt, 0)
}

func TestTicketClaimRefusesClosedAndMissing(t *testing.T) {
	repo, ctx := newStore(t, "ticketclaimrefuse")

	_, err := repo.TicketClaim(ctx, "g1", "nope", "staff1")
	assert.ErrorIs(t, err, repository.ErrNotFound)

	_, _, err = openOne(t, repo, ctx, "c1", 1)
	require.NoError(t, err)
	_, _, err = repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c1", ClosedBy: "staff1"})
	require.NoError(t, err)

	_, err = repo.TicketClaim(ctx, "g1", "c1", "staff1")
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestTicketCloseAndArchiveTransitions(t *testing.T) {
	repo, ctx := newStore(t, "ticketclose")

	deleted, _, err := openOne(t, repo, ctx, "c1", 0)
	require.NoError(t, err)
	id, opener, err := repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c1", ClosedBy: "staff1"})
	require.NoError(t, err)
	assert.Equal(t, deleted, id)
	assert.Equal(t, "member1", opener)

	row, _, err := repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusClosed, row.Status)
	require.NotNil(t, row.ClosedAt)

	_, _, err = openOne(t, repo, ctx, "c2", 0)
	require.NoError(t, err)
	_, _, err = repo.TicketClose(ctx, repository.CloseParams{
		GuildID: "g1", ChannelID: "c2", ClosedBy: "staff1", ArchivedChannelID: "c2",
	})
	require.NoError(t, err)
	row, _, err = repo.TicketGet(ctx, "g1", "c2")
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusArchived, row.Status)
	assert.Equal(t, "c2", row.ArchivedChannelID)
}

func TestTicketCloseTwiceKeepsTheFirstTimestamp(t *testing.T) {
	repo, ctx := newStore(t, "ticketcloseretry")

	_, _, err := openOne(t, repo, ctx, "c1", 0)
	require.NoError(t, err)
	_, _, err = repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c1", ClosedBy: "staff1"})
	require.NoError(t, err)
	row, _, err := repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	first := *row.ClosedAt

	id, opener, err := repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c1", ClosedBy: "staff2"})
	require.NoError(t, err)
	assert.Equal(t, row.ID, id)
	assert.Equal(t, "member1", opener)

	row, _, err = repo.TicketGet(ctx, "g1", "c1")
	require.NoError(t, err)
	assert.Equal(t, "staff1", row.ClosedBy)
	assert.WithinDuration(t, first, *row.ClosedAt, 0)
}

func TestTicketGetIsGuildScoped(t *testing.T) {
	repo, ctx := newStore(t, "ticketscope")

	_, _, err := openOne(t, repo, ctx, "c1", 0)
	require.NoError(t, err)

	_, found, err := repo.TicketGet(ctx, "otherguild", "c1")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTicketListFiltersAndPages(t *testing.T) {
	repo, ctx := newStore(t, "ticketlist")

	for i := range 5 {
		_, _, err := openOne(t, repo, ctx, "c"+strconv.Itoa(i), 0)
		require.NoError(t, err)
	}
	_, _, err := repo.TicketClose(ctx, repository.CloseParams{GuildID: "g1", ChannelID: "c0", ClosedBy: "staff1"})
	require.NoError(t, err)

	open, next, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Status: discorddata.StatusOpen})
	require.NoError(t, err)
	assert.Empty(t, next)
	assert.Len(t, open, 4)

	page1, next, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, next, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Limit: 2, Cursor: next})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.NotEqual(t, page1[0].ID, page2[0].ID)

	page3, next, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Limit: 2, Cursor: next})
	require.NoError(t, err)
	assert.Len(t, page3, 1)
	assert.Empty(t, next, "the last page carries no cursor")
}

func TestTicketListRejectsABadStatusOrCursor(t *testing.T) {
	repo, ctx := newStore(t, "ticketlistbad")

	_, _, err := repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Status: "exploded"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)

	_, _, err = repo.TicketList(ctx, repository.ListParams{GuildID: "g1", Cursor: "not-a-number"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestTranscriptPutAndGet(t *testing.T) {
	repo, ctx := newStore(t, "transcript")

	id, _, err := openOne(t, repo, ctx, "c1", 0)
	require.NoError(t, err)

	_, found, err := repo.TranscriptGet(ctx, id)
	require.NoError(t, err)
	assert.False(t, found, "a ticket with transcripts off has none stored")

	require.NoError(t, repo.TranscriptPut(ctx, id, "[12:00] member1: hello", 1))
	row, found, err := repo.TranscriptGet(ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "[12:00] member1: hello", row.Body)
	assert.Equal(t, 1, row.MessageCount)

	// A retried render replaces rather than appends.
	require.NoError(t, repo.TranscriptPut(ctx, id, "[12:00] member1: hello\n[12:01] staff1: hi", 2))
	row, _, err = repo.TranscriptGet(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, row.MessageCount)
	assert.Contains(t, row.Body, "staff1: hi")
}

func TestTranscriptPutRejectsUnknownTicketAndOversizeBody(t *testing.T) {
	repo, ctx := newStore(t, "transcriptbad")

	assert.ErrorIs(t, repo.TranscriptPut(ctx, 9999, "body", 1), repository.ErrNotFound)

	id, _, err := openOne(t, repo, ctx, "c1", 0)
	require.NoError(t, err)
	oversize := make([]byte, repository.MaxTranscriptBytes+1)
	assert.ErrorIs(t, repo.TranscriptPut(ctx, id, string(oversize), 1), repository.ErrInvalidInput)
}
