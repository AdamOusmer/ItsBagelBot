// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

// The desk handlers are exercised through a REAL discordapi.Client with a
// scripted transport rather than a hand-written stub of ticketREST. The point
// of these tests is the wire shape -- how many GETs the transcript makes, that
// the upload is multipart, that the archive PATCH carries parent_id -- and a
// stub of the interface would assert only that the handler called the method
// it obviously calls.

type recordedCall struct {
	method      string
	path        string
	query       string
	body        string
	contentType string
}

type scriptedTransport struct {
	mu    sync.Mutex
	calls []recordedCall
	// reply answers one request; the key is "METHOD /path".
	reply func(call recordedCall) (int, string)
}

func (s *scriptedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	call := recordedCall{
		method: r.Method, path: strings.TrimPrefix(r.URL.Path, "/api/v10"),
		query: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"),
	}
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		call.body = string(raw)
	}
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()

	status, body := 200, `{"id":"m-new"}`
	if s.reply != nil {
		status, body = s.reply(call)
	}
	rec := httptest.NewRecorder()
	rec.Code = status
	rec.Body.WriteString(body)
	return rec.Result(), nil
}

func (s *scriptedTransport) find(method, path string) []recordedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []recordedCall
	for _, c := range s.calls {
		if c.method == method && c.path == path {
			out = append(out, c)
		}
	}
	return out
}

func newTicketRPC(t *testing.T, reply func(recordedCall) (int, string)) (*ticketRPC, *scriptedTransport) {
	t.Helper()
	tr := &scriptedTransport{reply: reply}
	client := discapi.NewClient("bot-token")
	client.SetTransport(tr)
	return &ticketRPC{rest: client, log: zap.NewNop()}, tr
}

// messagePage renders n messages, newest first, with ids counting down from
// high so the before-cursor a page hands back is its own last id.
func messagePage(high, n int) string {
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := high - i
		items = append(items, fmt.Sprintf(
			`{"id":"%d","content":"line %d","timestamp":"2026-01-02T03:04:05+00:00","author":{"id":"u1","username":"ada"}}`,
			id, id))
	}
	return "[" + strings.Join(items, ",") + "]"
}

func TestTicketClosePagesHistoryUploadsAndArchives(t *testing.T) {
	h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.method == http.MethodGet && strings.HasSuffix(call.path, "/messages") {
			if strings.Contains(call.query, "before=") {
				return 200, messagePage(900, 3)
			}
			return 200, messagePage(1000, discapi.MessagePageMax)
		}
		return 200, `{"id":"m-new"}`
	})

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1", OpenerID: "u1",
		Transcript: true, LogChannelID: "log1", ArchiveCategoryID: "cat1",
		StaffRoleIDs: []string{"rmod", ""},
		Summary:      discordoutgress.TicketCloseSummary{Opener: "<@u1>", Closer: "Mod"},
	})

	if reply.Error != "" {
		t.Fatalf("close reply = %+v", reply)
	}
	if reply.MessageCount != discapi.MessagePageMax+3 {
		t.Fatalf("message count = %d", reply.MessageCount)
	}
	if reply.ArchivedChannelID != "c1" {
		t.Fatalf("archived channel = %q, want the channel itself", reply.ArchivedChannelID)
	}

	// Pagination: a full page is followed by one more call carrying the
	// before-cursor, and a short page ends the walk.
	pages := tr.find(http.MethodGet, "/channels/c1/messages")
	if len(pages) != 2 {
		t.Fatalf("history pages = %d, want 2", len(pages))
	}
	if strings.Contains(pages[0].query, "before=") {
		t.Fatalf("first page must have no cursor: %q", pages[0].query)
	}
	if !strings.Contains(pages[1].query, "before=901") {
		t.Fatalf("second page cursor = %q, want the first page's oldest id", pages[1].query)
	}

	// Transcript order: the render walks the pages oldest-first even though
	// Discord hands them back newest-first.
	if !strings.HasPrefix(reply.TranscriptBody, "[2026-01-02 03:04 UTC] ada: line 898\n") {
		t.Fatalf("transcript starts %q", firstLine(reply.TranscriptBody))
	}
	if !strings.HasSuffix(reply.TranscriptBody, "ada: line 1000\n") {
		t.Fatalf("transcript ends %q", reply.TranscriptBody[len(reply.TranscriptBody)-40:])
	}

	assertMultipartUpload(t, tr)
	assertArchivePatch(t, tr)

	// The channel was moved, not deleted.
	if got := tr.find(http.MethodDelete, "/channels/c1"); len(got) != 0 {
		t.Fatal("an archived ticket channel must survive")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func assertMultipartUpload(t *testing.T, tr *scriptedTransport) {
	t.Helper()
	uploads := tr.find(http.MethodPost, "/channels/log1/messages")
	if len(uploads) != 1 {
		t.Fatalf("log posts = %d, want 1", len(uploads))
	}
	mediaType, params, err := mime.ParseMediaType(uploads[0].contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q (%v)", uploads[0].contentType, err)
	}
	form, err := multipart.NewReader(strings.NewReader(uploads[0].body), params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("read form: %v", err)
	}
	if len(form.Value["payload_json"]) != 1 {
		t.Fatalf("payload_json parts = %v", form.Value)
	}
	payload := form.Value["payload_json"][0]
	for _, want := range []string{`"filename":"ticket-ada-1.txt"`, "Ticket closed", `"id":0`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload_json %q missing %q", payload, want)
		}
	}
	if len(form.File["files[0]"]) != 1 {
		t.Fatalf("file parts = %v", form.File)
	}
}

func assertArchivePatch(t *testing.T, tr *scriptedTransport) {
	t.Helper()
	patches := tr.find(http.MethodPatch, "/channels/c1")
	if len(patches) != 1 {
		t.Fatalf("channel patches = %d, want 1", len(patches))
	}
	body := patches[0].body
	for _, want := range []string{`"parent_id":"cat1"`, `"name":"closed-ticket-ada-1"`, `"id":"rmod"`, `"id":"u1"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("patch body %q missing %q", body, want)
		}
	}
	// The empty staff role id in the request must not become an overwrite on
	// the everyone-shaped id "".
	if strings.Contains(body, `"id":"","type":0,"allow"`) {
		t.Fatalf("patch body carries an empty-id overwrite: %q", body)
	}
}

func TestTicketCloseWithoutTranscriptOrArchiveDeletesTheChannel(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1",
		LogChannelID: "log1", Transcript: false,
	})

	if reply.Error != "" || reply.MessageCount != 0 || reply.TranscriptBody != "" {
		t.Fatalf("reply = %+v", reply)
	}
	if reply.ArchivedChannelID != "" {
		t.Fatalf("archived = %q, want empty when the channel is deleted", reply.ArchivedChannelID)
	}
	if got := tr.find(http.MethodGet, "/channels/c1/messages"); len(got) != 0 {
		t.Fatal("transcript off must not page the channel")
	}
	// The summary still posts, as a plain embed rather than an upload.
	posts := tr.find(http.MethodPost, "/channels/log1/messages")
	if len(posts) != 1 || strings.HasPrefix(posts[0].contentType, "multipart/") {
		t.Fatalf("log posts = %+v", posts)
	}
	if got := tr.find(http.MethodDelete, "/channels/c1"); len(got) != 1 {
		t.Fatalf("deletes = %d, want 1", len(got))
	}
}

func TestTicketCloseWithoutALogChannelPostsNothing(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1"})

	if reply.Error != "" {
		t.Fatalf("reply = %+v", reply)
	}
	for _, call := range tr.calls {
		if call.method == http.MethodPost {
			t.Fatalf("unexpected post: %+v", call)
		}
	}
}

func TestTicketCloseMapsForbiddenOntoTheCode(t *testing.T) {
	h, _ := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.method == http.MethodPatch {
			return 403, `{"message":"Missing Permissions"}`
		}
		return 200, `{"id":"m-new"}`
	})

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "t", ArchiveCategoryID: "cat1",
	})

	if reply.Code != outgressrpc.CodeForbidden {
		t.Fatalf("code = %q, want %q (reply %+v)", reply.Code, outgressrpc.CodeForbidden, reply)
	}
	if reply.Error == "" {
		t.Fatal("the message travels alongside the code")
	}
}

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
	if err := json.Unmarshal([]byte(edits[0].body), &patch); err != nil {
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

func TestPageLimitNeverOvershootsTheCap(t *testing.T) {
	if got := pageLimit(0); got != discapi.MessagePageMax {
		t.Fatalf("pageLimit(0) = %d", got)
	}
	left := ddiscord.TranscriptMessageCap - 40
	if got := pageLimit(left); got != 40 {
		t.Fatalf("pageLimit(%d) = %d, want 40", left, got)
	}
}

func TestCollectStopsAtTheMessageCap(t *testing.T) {
	// Every page is full, so only the cap ends the walk.
	next := 100000
	h, tr := newTicketRPC(t, func(recordedCall) (int, string) {
		next -= discapi.MessagePageMax
		return 200, messagePage(next+discapi.MessagePageMax, discapi.MessagePageMax)
	})

	got, err := h.collect(context.Background(), "c1")

	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(got) != ddiscord.TranscriptMessageCap {
		t.Fatalf("collected = %d, want the cap %d", len(got), ddiscord.TranscriptMessageCap)
	}
	want := ddiscord.TranscriptMessageCap / discapi.MessagePageMax
	if pages := tr.find(http.MethodGet, "/channels/c1/messages"); len(pages) != want {
		t.Fatalf("pages = %d, want %d", len(pages), want)
	}
}

func TestCollectDegradesToThePartialTranscript(t *testing.T) {
	calls := 0
	h, _ := newTicketRPC(t, func(recordedCall) (int, string) {
		calls++
		if calls == 1 {
			return 200, messagePage(1000, discapi.MessagePageMax)
		}
		return 500, `{"message":"boom"}`
	})

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", Transcript: true, LogChannelID: "log1",
	})

	if reply.MessageCount != discapi.MessagePageMax {
		t.Fatalf("count = %d, want the pages that did arrive", reply.MessageCount)
	}
	if !strings.Contains(reply.TranscriptBody, "line 1000") {
		t.Fatal("a failed page must not throw away the pages that succeeded")
	}
}

func TestArchivedNameLeavesAnUnknownNameAlone(t *testing.T) {
	if got := archivedName(""); got != "" {
		t.Fatalf("archivedName(\"\") = %q; an empty name means ModifyChannel leaves it alone", got)
	}
	if got := archivedName("ticket-ada-" + strconv.Itoa(1)); got != "closed-ticket-ada-1" {
		t.Fatalf("archivedName = %q", got)
	}
}
