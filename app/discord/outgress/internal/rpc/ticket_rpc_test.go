// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
)

const missingPermissions = `{"message":"Missing Permissions"}`

func serveTickets(rest *discapi.Client, _ kv.LiveStore, wire Wiring) error {
	return SubscribeTickets(rest, TicketDeps{BotID: "bot9"}, wire)
}

func ticketOpenCases() []discordCase {
	created := answer{status: http.StatusOK, body: `{"id":"c-new","name":"ticket-ada-1"}`}
	both := []string{"POST /guilds/g1/channels", "POST /channels/c-new/messages"}
	return []discordCase{{
		name: "creates the channel and then posts the card",
		verb: "ticket.open",
		req: discordoutgress.TicketOpenRequest{
			GuildID: "g1", Name: "ticket-ada-1", ParentID: "cat1", Content: "<@u1>",
			Embed:   ddiscord.Embed{Title: "Ticket", Description: "Describe your issue", Color: 7},
			Buttons: []ddiscord.ButtonSpec{{Style: 2, Label: "Claim", CustomID: discapi.CustomTicketClaim}},
		},
		routes: map[string]answer{"POST /guilds/g1/channels": created},
		want:   discordoutgress.TicketOpenReply{ChannelID: "c-new", MessageID: "m-new"},
		calls:  both,
		write:  "POST /channels/c-new/messages",
		body: `{"content":"<@u1>","embeds":[{"title":"Ticket","description":"Describe your issue","color":7}],` +
			`"components":[{"type":1,"components":[{"type":2,"style":2,"label":"Claim","custom_id":"` +
			discapi.CustomTicketClaim + `"}]}]}`,
	}, {
		name: "reports the orphan channel when the card is refused",
		verb: "ticket.open",
		req:  discordoutgress.TicketOpenRequest{GuildID: "g1", Name: "t"},
		routes: map[string]answer{
			"POST /guilds/g1/channels":      created,
			"POST /channels/c-new/messages": {status: http.StatusForbidden, body: missingPermissions},
		},
		want: discordoutgress.TicketOpenReply{
			ChannelID: "c-new", Error: "discord: forbidden: " + missingPermissions, Code: outgressrpc.CodeForbidden,
		},
		calls: both,
	}, {
		name:   "reports a refused channel create",
		verb:   "ticket.open",
		req:    discordoutgress.TicketOpenRequest{GuildID: "g1", Name: "t"},
		routes: map[string]answer{"POST /guilds/g1/channels": {status: http.StatusForbidden, body: missingPermissions}},
		want: discordoutgress.TicketOpenReply{
			Error: "discord: forbidden: " + missingPermissions, Code: outgressrpc.CodeForbidden,
		},
		calls: both[:1],
	}}
}

func ticketClaimCases() []discordCase {
	claim := discordoutgress.TicketClaimRequest{
		GuildID: "g1", ChannelID: "c1", MessageID: "m1", Content: "<@u1>", Note: "Mod claimed this ticket.",
		Embed: ddiscord.Embed{Title: "Ticket", Description: "Claimed by Mod"},
	}
	edit := []string{"GET /channels/c1", "PATCH /channels/c1/messages/m1"}
	withNote := slices.Concat(edit, []string{"POST /channels/c1/messages"})
	return []discordCase{{
		name:  "edits the claimed card and posts the note",
		verb:  "ticket.claim",
		req:   claim,
		want:  discordoutgress.TicketClaimReply{},
		calls: withNote,
		write: "PATCH /channels/c1/messages/m1",
		body:  `{"content":"<@u1>","embeds":[{"title":"Ticket","description":"Claimed by Mod"}]}`,
	}, {
		name: "edits the card without posting an empty note",
		verb: "ticket.claim",
		req: discordoutgress.TicketClaimRequest{
			GuildID: "g1", ChannelID: "c1", MessageID: "m1", Embed: ddiscord.Embed{Title: "Ticket"},
		},
		want:  discordoutgress.TicketClaimReply{},
		calls: edit,
	}, {
		name:   "keeps the claim when the note is refused",
		verb:   "ticket.claim",
		req:    claim,
		routes: map[string]answer{"POST /channels/c1/messages": {status: http.StatusForbidden}},
		want:   discordoutgress.TicketClaimReply{},
		calls:  withNote,
	}, {
		name:   "reports a refused card edit and posts no note",
		verb:   "ticket.claim",
		req:    claim,
		routes: map[string]answer{"PATCH /channels/c1/messages/m1": {status: http.StatusForbidden, body: missingPermissions}},
		want: discordoutgress.TicketClaimReply{
			Error: "discord: forbidden: " + missingPermissions, Code: outgressrpc.CodeForbidden,
		},
		calls: edit,
	}, {
		name: "ignores a claim without a card",
		verb: "ticket.claim",
		req:  discordoutgress.TicketClaimRequest{ChannelID: "c1"},
		want: discordoutgress.TicketClaimReply{},
	}}
}

func ticketAddCases() []discordCase {
	add := discordoutgress.TicketMemberAddRequest{GuildID: "g1", ChannelID: "c1", UserID: "u2"}
	calls := []string{"GET /channels/c1", "PUT /channels/c1/permissions/u2"}
	failed := func(name string, got answer, want discordoutgress.TicketMemberAddReply) discordCase {
		return discordCase{
			name: name, verb: "ticket.add", req: add, want: want, calls: calls,
			routes: map[string]answer{"PUT /channels/c1/permissions/u2": got},
		}
	}
	return []discordCase{{
		name:  "grants a member one overwrite without rewriting the channel",
		verb:  "ticket.add",
		req:   add,
		want:  discordoutgress.TicketMemberAddReply{},
		calls: calls,
		write: "PUT /channels/c1/permissions/u2",
		body:  `{"id":"u2","type":1,"allow":"68608","deny":"0"}`,
	},
		failed("maps a bad request onto invalid", answer{status: http.StatusBadRequest, body: "bad"},
			addFailure("discord: bad request: bad", outgressrpc.CodeInvalid)),
		failed("maps lost credentials onto discord_unavailable", answer{status: http.StatusUnauthorized},
			addFailure(discapi.ErrAuth.Error(), outgressrpc.CodeDiscordUnavailable)),
		failed("maps a refusal onto forbidden", answer{status: http.StatusForbidden, body: missingPermissions},
			addFailure("discord: forbidden: "+missingPermissions, outgressrpc.CodeForbidden)),
		failed("maps a missing channel onto not_found", answer{status: http.StatusNotFound},
			addFailure(discapi.ErrChannelNotFound.Error(), outgressrpc.CodeNotFound)),
		failed("maps a rate limit onto rate_limited", answer{status: http.StatusTooManyRequests, body: "slow down"},
			addFailure("discord: rate limited: slow down", outgressrpc.CodeRateLimited)),
		failed("maps a deadline onto timeout", answer{err: context.DeadlineExceeded},
			addFailure(`Put "https://discord.com/api/v10/channels/c1/permissions/u2": context deadline exceeded`, outgressrpc.CodeTimeout)),
		failed("maps an unclassified failure onto unknown", answer{status: http.StatusBadGateway, body: "boom"},
			addFailure("discord: api rejected request (502): boom", outgressrpc.CodeUnknown)),
	}
}

func ticketPanelCases() []discordCase {
	return []discordCase{{
		name: "reports a refused desk panel post",
		verb: "ticket.panel",
		req:  discordoutgress.TicketPanelRequest{GuildID: "g1", ChannelID: "desk1"},
		routes: map[string]answer{
			"POST /channels/desk1/messages": {status: http.StatusForbidden, body: missingPermissions},
		},
		want: discordoutgress.TicketPanelReply{
			Error: "discord: forbidden: " + missingPermissions, Code: outgressrpc.CodeForbidden,
		},
		calls: []string{"GET /channels/desk1", "POST /channels/desk1/messages"},
	}}
}

func addFailure(message string, code rpc.Code) discordoutgress.TicketMemberAddReply {
	return discordoutgress.TicketMemberAddReply{Error: message, Code: code}
}

func TestTicketVerbsReachDiscordAndAnswerWithAnHonestCode(t *testing.T) {
	for _, tc := range slices.Concat(ticketOpenCases(), ticketClaimCases(), ticketAddCases(), ticketPanelCases()) {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected(t), tc.exchange(t, serveTickets))
		})
	}
}

func TestTicketOpenGrantsTheBotItsOwnOverwrite(t *testing.T) {
	h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.path == "/guilds/g1/channels" {
			return 200, `{"id":"c-new"}`
		}
		return 200, `{"id":"m-new"}`
	})

	h.open(context.Background(), discordoutgress.TicketOpenRequest{
		GuildID: "g1", Name: "ticket-ada",
		Overwrites: []discapi.PermissionOverwrite{{ID: "g1", Type: 0, Allow: "0", Deny: "1024"}},
	})

	creates := tr.find(http.MethodPost, "/guilds/g1/channels")
	if len(creates) != 1 {
		t.Fatalf("creates = %d", len(creates))
	}
	body := creates[0].body
	want := `{"id":"bot9","type":1,"allow":"` + permTicketBotBits + `","deny":"0"}`
	if !strings.Contains(body, want) {
		t.Fatalf("create body %q missing the bot overwrite %q", body, want)
	}
	if !strings.Contains(body, `{"id":"g1","type":0,"allow":"0","deny":"1024"}`) {
		t.Fatalf("create body %q dropped the engine overwrite", body)
	}
}

func TestTicketPanelReturnsThePostedMessageID(t *testing.T) {
	h, tr := newTicketRPC(t, func(recordedCall) (int, string) { return 200, `{"id":"m-panel"}` })

	reply := h.panel(context.Background(), discordoutgress.TicketPanelRequest{
		GuildID: "g1", ChannelID: "desk1",
		Buttons: []ddiscord.ButtonSpec{{Style: 1, Label: "Open a ticket", CustomID: discapi.CustomTicketOpen}},
	})

	if reply.Error != "" || reply.MessageID != "m-panel" {
		t.Fatalf("reply = %+v", reply)
	}
	posts := tr.find(http.MethodPost, "/channels/desk1/messages")
	if len(posts) != 1 || !strings.Contains(posts[0].body, discapi.CustomTicketOpen) {
		t.Fatalf("panel post = %+v", posts)
	}
}

func TestTicketPanelRefusesAnEmptyChannel(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.panel(context.Background(), discordoutgress.TicketPanelRequest{GuildID: "g1"})

	if reply.Code != outgressrpc.CodeInvalid || reply.MessageID != "" {
		t.Fatalf("reply = %+v", reply)
	}
	if len(tr.calls) != 0 {
		t.Fatalf("a refused panel must not call Discord: %+v", tr.calls)
	}
}
