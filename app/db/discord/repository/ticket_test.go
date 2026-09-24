// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/db/discord/ent/ticket"
	"ItsBagelBot/app/db/discord/repository"
	"ItsBagelBot/internal/domain/rpc/discorddata"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ticketFixture struct {
	t    *testing.T
	repo *repository.Store
	ctx  context.Context
}

func newTicketFixture(t *testing.T, name string) ticketFixture {
	t.Helper()
	repo, ctx := newStore(t, name)
	return ticketFixture{t: t, repo: repo, ctx: ctx}
}

func newConcurrentTicketFixture(t *testing.T, name string) ticketFixture {
	t.Helper()
	repo, ctx := newConcurrentStore(t, name)
	return ticketFixture{t: t, repo: repo, ctx: ctx}
}

func tk(guildID, channelID string) repository.TicketKey {
	return repository.TicketKey{GuildID: guildID, ChannelID: channelID}
}

func mk(guildID, memberID string) repository.MemberKey {
	return repository.MemberKey{GuildID: guildID, MemberID: memberID}
}

func (f ticketFixture) openOne(channelID string, limit int) (int, int, error) {
	f.t.Helper()
	return f.repo.TicketOpen(f.ctx, repository.OpenParams{
		Key:      tk("g1", channelID),
		OpenerID: "member1",
		Subject:  "help me",
		Limit:    limit,
	})
}

func TestTicketOpenRecordsAndCounts(t *testing.T) {
	f := newTicketFixture(t, "ticketopen")

	id, count, err := f.openOne("c1", 3)
	require.NoError(t, err)
	assert.NotZero(t, id)
	assert.Equal(t, 1, count)

	row, found, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ticket.StatusOpen, row.Status)
	assert.Equal(t, "member1", row.OpenerID)
	assert.Equal(t, "help me", row.Subject)
}

func TestTicketOpenRefusesPastTheLimit(t *testing.T) {
	f := newTicketFixture(t, "ticketlimit")

	_, _, err := f.openOne("c1", 2)
	require.NoError(t, err)
	_, count, err := f.openOne("c2", 2)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	_, count, err = f.openOne("c3", 2)
	assert.ErrorIs(t, err, repository.ErrOpenLimit)
	assert.Equal(t, 2, count, "the refusal carries the count the caller names back")

	_, found, err := f.repo.TicketGet(f.ctx, tk("g1", "c3"))
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTicketOpenLimitZeroIsUnlimited(t *testing.T) {
	f := newTicketFixture(t, "ticketnolimit")

	for i := range 4 {
		_, _, err := f.openOne("c"+strconv.Itoa(i), 0)
		require.NoError(t, err)
	}
	rows, _, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1"})
	require.NoError(t, err)
	assert.Len(t, rows, 4)
}

func TestTicketOpenIsIdempotentPerChannel(t *testing.T) {
	f := newTicketFixture(t, "ticketidempotent")

	first, _, err := f.openOne("c1", 1)
	require.NoError(t, err)

	second, _, err := f.openOne("c1", 1)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestTicketOpenClosedTicketsDoNotCountAgainstTheLimit(t *testing.T) {
	f := newTicketFixture(t, "ticketlimitclosed")

	_, _, err := f.openOne("c1", 1)
	require.NoError(t, err)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c1"), ClosedBy: "staff1"})
	require.NoError(t, err)

	_, count, err := f.openOne("c2", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTicketClaimTransition(t *testing.T) {
	f := newTicketFixture(t, "ticketclaim")

	id, _, err := f.openOne("c1", 1)
	require.NoError(t, err)

	claimed, err := f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("g1", "c1"), StaffID: "staff1"})
	require.NoError(t, err)
	assert.Equal(t, id, claimed)

	row, _, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusClaimed, row.Status)
	assert.Equal(t, "staff1", row.ClaimedBy)
	require.NotNil(t, row.ClaimedAt)
	firstClaim := *row.ClaimedAt

	_, err = f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("g1", "c1"), StaffID: "staff2"})
	require.NoError(t, err)
	row, _, err = f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	assert.Equal(t, "staff2", row.ClaimedBy)
	assert.WithinDuration(t, firstClaim, *row.ClaimedAt, 0)
}

func TestTicketClaimRefusesClosedAndMissing(t *testing.T) {
	f := newTicketFixture(t, "ticketclaimrefuse")

	_, err := f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("g1", "nope"), StaffID: "staff1"})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	_, _, err = f.openOne("c1", 1)
	require.NoError(t, err)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c1"), ClosedBy: "staff1"})
	require.NoError(t, err)

	_, err = f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("g1", "c1"), StaffID: "staff1"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestTicketCloseAndArchiveTransitions(t *testing.T) {
	f := newTicketFixture(t, "ticketclose")

	deleted, _, err := f.openOne("c1", 0)
	require.NoError(t, err)
	id, opener, err := f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c1"), ClosedBy: "staff1"})
	require.NoError(t, err)
	assert.Equal(t, deleted, id)
	assert.Equal(t, "member1", opener)

	row, _, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusClosed, row.Status)
	require.NotNil(t, row.ClosedAt)

	_, _, err = f.openOne("c2", 0)
	require.NoError(t, err)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{
		Key: tk("g1", "c2"), ClosedBy: "staff1", ArchivedChannelID: "c2",
	})
	require.NoError(t, err)
	row, _, err = f.repo.TicketGet(f.ctx, tk("g1", "c2"))
	require.NoError(t, err)
	assert.Equal(t, ticket.StatusArchived, row.Status)
	assert.Equal(t, "c2", row.ArchivedChannelID)
}

func TestTicketCloseTwiceKeepsTheFirstTimestamp(t *testing.T) {
	f := newTicketFixture(t, "ticketcloseretry")

	_, _, err := f.openOne("c1", 0)
	require.NoError(t, err)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c1"), ClosedBy: "staff1"})
	require.NoError(t, err)
	row, _, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	first := *row.ClosedAt

	id, opener, err := f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c1"), ClosedBy: "staff2"})
	require.NoError(t, err)
	assert.Equal(t, row.ID, id)
	assert.Equal(t, "member1", opener)

	row, _, err = f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	assert.Equal(t, "staff1", row.ClosedBy)
	assert.WithinDuration(t, first, *row.ClosedAt, 0)
}

func TestTicketGetIsGuildScoped(t *testing.T) {
	f := newTicketFixture(t, "ticketscope")

	_, _, err := f.openOne("c1", 0)
	require.NoError(t, err)

	_, found, err := f.repo.TicketGet(f.ctx, tk("otherguild", "c1"))
	require.NoError(t, err)
	assert.False(t, found)
}

func TestTicketListFiltersAndPages(t *testing.T) {
	f := newTicketFixture(t, "ticketlist")

	for i := range 5 {
		_, _, err := f.openOne("c"+strconv.Itoa(i), 0)
		require.NoError(t, err)
	}
	_, _, err := f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c0"), ClosedBy: "staff1"})
	require.NoError(t, err)

	open, next, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Status: discorddata.StatusOpen})
	require.NoError(t, err)
	assert.Empty(t, next)
	assert.Len(t, open, 4)

	page1, next, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, next)

	page2, next, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Limit: 2, Cursor: next})
	require.NoError(t, err)
	require.Len(t, page2, 2)
	assert.NotEqual(t, page1[0].ID, page2[0].ID)

	page3, next, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Limit: 2, Cursor: next})
	require.NoError(t, err)
	assert.Len(t, page3, 1)
	assert.Empty(t, next, "the last page carries no cursor")
}

func TestTicketListRejectsABadStatusOrCursor(t *testing.T) {
	f := newTicketFixture(t, "ticketlistbad")

	_, _, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Status: "exploded"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)

	_, _, err = f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Cursor: "not-a-number"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestTranscriptPutAndGet(t *testing.T) {
	f := newTicketFixture(t, "transcript")

	id, _, err := f.openOne("c1", 0)
	require.NoError(t, err)

	_, found, err := f.repo.TranscriptGet(f.ctx, id)
	require.NoError(t, err)
	assert.False(t, found, "a ticket with transcripts off has none stored")

	require.NoError(t, f.repo.TranscriptPut(f.ctx, id, "[12:00] member1: hello", 1))
	row, found, err := f.repo.TranscriptGet(f.ctx, id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "[12:00] member1: hello", row.Body)
	assert.Equal(t, 1, row.MessageCount)

	require.NoError(t, f.repo.TranscriptPut(f.ctx, id, "[12:00] member1: hello\n[12:01] staff1: hi", 2))
	row, _, err = f.repo.TranscriptGet(f.ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, row.MessageCount)
	assert.Contains(t, row.Body, "staff1: hi")
}

func TestTranscriptPutRejectsUnknownTicketAndOversizeBody(t *testing.T) {
	f := newTicketFixture(t, "transcriptbad")

	assert.ErrorIs(t, f.repo.TranscriptPut(f.ctx, 9999, "body", 1), repository.ErrNotFound)

	id, _, err := f.openOne("c1", 0)
	require.NoError(t, err)
	oversize := make([]byte, repository.MaxTranscriptBytes+1)
	assert.ErrorIs(t, f.repo.TranscriptPut(f.ctx, id, string(oversize), 1), repository.ErrInvalidInput)
}

func TestTicketOpenLimitHoldsUnderConcurrentOpens(t *testing.T) {
	f := newConcurrentTicketFixture(t, "ticketopenrace")

	_, _, err := f.openOne("c0", 2)
	require.NoError(t, err)

	const callers = 4
	var wg sync.WaitGroup
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			_, _, errs[i] = f.openOne("c"+strconv.Itoa(i+1), 2)
		}()
	}
	wg.Wait()

	opened := 0
	for i := range callers {
		if errs[i] == nil {
			opened++
			continue
		}
		require.ErrorIs(t, errs[i], repository.ErrOpenLimit, "opener %d", i)
	}
	assert.Equal(t, 1, opened, "the limit of two leaves room for exactly one more open")

	rows, _, err := f.repo.TicketList(f.ctx, repository.ListParams{GuildID: "g1", Status: string(ticket.StatusOpen)})
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestTicketVerbsRequireTheGuild(t *testing.T) {
	f := newTicketFixture(t, "ticketguildrequired")
	_, _, err := f.openOne("c1", 0)
	require.NoError(t, err)

	_, _, err = f.repo.TicketGet(f.ctx, tk("", "c1"))
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("", "c1"), StaffID: "staff1"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("", "c1"), ClosedBy: "staff1"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestTicketVerbsRefuseAnotherGuildsChannel(t *testing.T) {
	f := newTicketFixture(t, "ticketwrongguild")
	_, _, err := f.openOne("c1", 0)
	require.NoError(t, err)

	_, err = f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("otherguild", "c1"), StaffID: "staff1"})
	assert.ErrorIs(t, err, repository.ErrNotFound)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{
		Key: tk("otherguild", "c1"), ClosedBy: "staff1",
	})
	assert.ErrorIs(t, err, repository.ErrNotFound)

	row, found, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ticket.StatusOpen, row.Status, "neither refused verb may have changed the row")
}

func TestTicketOpenStoresThePanelMessageID(t *testing.T) {
	f := newTicketFixture(t, "ticketpanelmsg")

	_, _, err := f.repo.TicketOpen(f.ctx, repository.OpenParams{
		Key: tk("g1", "c1"), OpenerID: "member1", PanelMessageID: "m1",
	})
	require.NoError(t, err)

	row, found, err := f.repo.TicketGet(f.ctx, tk("g1", "c1"))
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "m1", row.PanelMessageID, "the claim edit needs the card it must patch")
}

func TestTicketOpenCountCountsLiveTicketsOnly(t *testing.T) {
	f := newTicketFixture(t, "ticketcount")

	n, err := f.repo.TicketOpenCount(f.ctx, mk("g1", "member1"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	_, _, err = f.openOne("c1", 0)
	require.NoError(t, err)
	_, _, err = f.openOne("c2", 0)
	require.NoError(t, err)

	n, err = f.repo.TicketOpenCount(f.ctx, mk("g1", "member1"))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = f.repo.TicketClaim(f.ctx, repository.ClaimParams{Key: tk("g1", "c1"), StaffID: "mod1"})
	require.NoError(t, err)
	_, _, err = f.repo.TicketClose(f.ctx, repository.CloseParams{Key: tk("g1", "c2"), ClosedBy: "mod1"})
	require.NoError(t, err)

	n, err = f.repo.TicketOpenCount(f.ctx, mk("g1", "member1"))
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	n, err = f.repo.TicketOpenCount(f.ctx, mk("g1", "member2"))
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}
