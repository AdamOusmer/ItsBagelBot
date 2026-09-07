// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"net/http"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/codec"
)

func TestTicketOpenCreatesThenPostsTheCard(t *testing.T) {
	h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.method == http.MethodPost && call.path == "/guilds/g1/channels" {
			return 200, `{"id":"c-new","name":"ticket-ada-1"}`
		}
		return 200, `{"id":"m-new"}`
	})

	reply := h.open(context.Background(), discordoutgress.TicketOpenRequest{
		GuildID: "g1", Name: "ticket-ada-1", ParentID: "cat1", Content: "<@u1>",
		Embed:   ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: "Ada"}),
		Buttons: []ddiscord.ButtonSpec{{Style: 2, Label: "Claim", CustomID: discapi.CustomTicketClaim}},
	})

	if reply.Error != "" || reply.ChannelID != "c-new" || reply.MessageID != "m-new" {
		t.Fatalf("reply = %+v", reply)
	}
	posts := tr.find(http.MethodPost, "/channels/c-new/messages")
	if len(posts) != 1 || !strings.Contains(posts[0].body, discapi.CustomTicketClaim) {
		t.Fatalf("card post = %+v", posts)
	}
}

func TestTicketOpenReportsTheChannelEvenWhenTheCardFails(t *testing.T) {
	h, _ := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.path == "/guilds/g1/channels" {
			return 200, `{"id":"c-new"}`
		}
		return 403, `{"message":"Missing Permissions"}`
	})

	reply := h.open(context.Background(), discordoutgress.TicketOpenRequest{GuildID: "g1", Name: "t"})

	if reply.ChannelID != "c-new" {
		t.Fatal("the caller must learn about the orphan channel so it can roll it back")
	}
	if reply.Code != outgressrpc.CodeForbidden {
		t.Fatalf("code = %q", reply.Code)
	}
}

func TestTicketClaimEditsTheCardAndPostsTheNote(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.claim(context.Background(), discordoutgress.TicketClaimRequest{
		ChannelID: "c1", MessageID: "m1", Content: "<@u1>", Note: "Mod claimed this ticket.",
		Embed: ddiscord.TicketOpenedEmbed(ddiscord.TicketOpened{Opener: "<@u1>", ClaimedBy: "Mod"}),
	})

	if reply.Error != "" {
		t.Fatalf("reply = %+v", reply)
	}
	edits := tr.find(http.MethodPatch, "/channels/c1/messages/m1")
	if len(edits) != 1 || !strings.Contains(edits[0].body, "Claimed by Mod") {
		t.Fatalf("edit = %+v", edits)
	}
	// Decoded rather than substring-matched: the JSON encoder escapes "<" to
	// \u003c, so a mention never appears literally in the wire body.
	var patch struct {
		Content string `json:"content"`
	}
	if err := codec.Unmarshal([]byte(edits[0].body), &patch); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patch.Content != "<@u1>" {
		t.Fatalf("content = %q; Discord's PATCH replaces it rather than leaving it alone", patch.Content)
	}
	if got := tr.find(http.MethodPost, "/channels/c1/messages"); len(got) != 1 {
		t.Fatalf("notes = %d, want 1", len(got))
	}
}

func TestTicketClaimWithoutACardIsANoOp(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.claim(context.Background(), discordoutgress.TicketClaimRequest{ChannelID: "c1"})

	if reply.Error != "" {
		t.Fatalf("reply = %+v", reply)
	}
	if len(tr.calls) != 0 {
		t.Fatalf("calls = %+v, want none", tr.calls)
	}
}

func TestTicketAddWritesOneOverwrite(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.add(context.Background(), discordoutgress.TicketMemberAddRequest{ChannelID: "c1", UserID: "u2"})

	if reply.Error != "" {
		t.Fatalf("reply = %+v", reply)
	}
	puts := tr.find(http.MethodPut, "/channels/c1/permissions/u2")
	if len(puts) != 1 {
		t.Fatalf("overwrite writes = %+v", tr.calls)
	}
	if !strings.Contains(puts[0].body, `"allow":"`+permTicketMemberBits+`"`) {
		t.Fatalf("overwrite body = %q", puts[0].body)
	}
	// One PUT, never a channel PATCH: the PATCH would replace every other
	// overwrite on the private channel.
	if got := tr.find(http.MethodPatch, "/channels/c1"); len(got) != 0 {
		t.Fatal("adding a member must not rewrite the whole overwrite array")
	}
}

// The bot needs an explicit overwrite on the ticket channel it just created:
// a staff-only ticket category that denies @everyone denies the bot too, and
// the desk then 403s on every call into the channel it opened. Only outgress
// knows the application id, so the overwrite is appended here rather than by
// the engine that built the rest of the set.
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
	// The engine'"'"'s own overwrites survive alongside it.
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
