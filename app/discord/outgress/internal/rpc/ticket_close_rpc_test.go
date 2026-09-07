// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
)

// pagedHistory scripts a channel whose history is one full page followed by a
// short one, which is what makes the transcript walk take two GETs and stop.
func pagedHistory(call recordedCall) (int, string) {
	if call.method != http.MethodGet || !strings.HasSuffix(call.path, "/messages") {
		return 200, `{"id":"m-new"}`
	}
	if strings.Contains(call.query, "before=") {
		return 200, messagePage(900, 3)
	}
	return 200, messagePage(1000, discapi.MessagePageMax)
}

// assertHistoryPaging pins the walk itself: one uncursored page, then one
// carrying the oldest id of the page before it. A missing cursor re-reads the
// same page forever; a cursor on the first call skips the newest messages.
func assertHistoryPaging(t *testing.T, tr *scriptedTransport) {
	t.Helper()
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
}

// assertTranscriptOrder: the render walks the pages oldest-first even though
// Discord hands them back newest-first, so a transcript reads as the
// conversation happened.
func assertTranscriptOrder(t *testing.T, body string) {
	t.Helper()
	if !strings.HasPrefix(body, "[2026-01-02 03:04 UTC] ada: line 898\n") {
		t.Fatalf("transcript starts %q", firstLine(body))
	}
	if !strings.HasSuffix(body, "ada: line 1000\n") {
		t.Fatalf("transcript ends %q", body[len(body)-40:])
	}
}

func TestTicketClosePagesHistoryUploadsAndArchives(t *testing.T) {
	h, tr := newTicketRPC(t, pagedHistory)

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

	assertHistoryPaging(t, tr)
	assertTranscriptOrder(t, reply.TranscriptBody)
	assertMultipartUpload(t, tr)
	assertArchivePatch(t, tr)

	// The channel was moved, not deleted.
	if got := tr.find(http.MethodDelete, "/channels/c1"); len(got) != 0 {
		t.Fatal("an archived ticket channel must survive")
	}
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

// An overwrite'"'"'s unused half is the string "0", never "". Discord'"'"'s overwrite
// object types allow and deny as strings; an empty one is rejected outright by
// newer API versions and read as "leave the existing value" by older ones,
// which on the archive PATCH means a deny lands on top of an allow we meant to
// clear. decode.OverwriteAllow/OverwriteDeny use the same convention.
func TestArchiveOverwritesCarryBothHalves(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1", OpenerID: "u1",
		ArchiveCategoryID: "cat1", StaffRoleIDs: []string{"rmod"},
	})

	patches := tr.find(http.MethodPatch, "/channels/c1")
	if len(patches) != 1 {
		t.Fatalf("patches = %d", len(patches))
	}
	for _, want := range []string{
		`{"id":"g1","type":0,"allow":"0","deny":"1024"}`,
		`{"id":"u1","type":1,"allow":"0","deny":"1024"}`,
		`{"id":"rmod","type":0,"allow":"66560","deny":"0"}`,
	} {
		if !strings.Contains(patches[0].body, want) {
			t.Fatalf("patch body %q missing %q", patches[0].body, want)
		}
	}
	if strings.Contains(patches[0].body, `"allow":""`) || strings.Contains(patches[0].body, `"deny":""`) {
		t.Fatalf("patch body carries an empty permission half: %q", patches[0].body)
	}
}

// The channel is disposed of BEFORE the summary posts. The summary is the step
// most likely to fail slowly (a missing, forbidden or rate-limited log
// channel), and when it ran first a failure there left the ticket channel
// sitting in the open category with a closed row behind it.
func TestTicketCloseDisposesBeforePostingTheSummary(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1",
		LogChannelID: "log1", ArchiveCategoryID: "cat1",
	})

	dispose := tr.indexOf(http.MethodPatch, "/channels/c1")
	summary := tr.indexOf(http.MethodPost, "/channels/log1/messages")
	if dispose < 0 || summary < 0 {
		t.Fatalf("calls = dispose %d, summary %d", dispose, summary)
	}
	if dispose > summary {
		t.Fatal("the channel must be archived before the summary posts")
	}
}

// A retried close must not stack a second card and a second transcript upload
// in the log channel. The memo is keyed on the ticket row id.
func TestTicketCloseSummaryPostsOncePerTicket(t *testing.T) {
	h, tr := newTicketRPC(t, nil)
	h.memo = newOnceMemo()
	req := discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1", TicketID: 42,
		LogChannelID: "log1", ArchiveCategoryID: "cat1",
	}

	h.close(context.Background(), req)
	h.close(context.Background(), req)

	if posts := tr.find(http.MethodPost, "/channels/log1/messages"); len(posts) != 1 {
		t.Fatalf("log posts = %d, want 1 across two closes", len(posts))
	}
}

// A ticket with no row id (the pure-Valkey fallback) has nothing to key the
// memo on, so it posts every time rather than never.
func TestTicketCloseWithoutARowIDAlwaysPosts(t *testing.T) {
	h, tr := newTicketRPC(t, nil)
	h.memo = newOnceMemo()
	req := discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", LogChannelID: "log1", ArchiveCategoryID: "cat1",
	}

	h.close(context.Background(), req)
	h.close(context.Background(), req)

	if posts := tr.find(http.MethodPost, "/channels/log1/messages"); len(posts) != 2 {
		t.Fatalf("log posts = %d, want one per close", len(posts))
	}
}

// The upload is the half that fails on its own -- a payload too large, or a
// proxy that rejects multipart. When the card only ever travelled attached to
// the file, that failure took the close record with it.
func TestTicketCloseFallsBackToAnEmbedWhenTheUploadFails(t *testing.T) {
	uploaded := false
	h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.method == http.MethodGet && strings.HasSuffix(call.path, "/messages") {
			return 200, messagePage(1000, 2)
		}
		if strings.HasPrefix(call.contentType, "multipart/") {
			uploaded = true
			return 400, `{"message":"Request entity too large"}`
		}
		return 200, `{"id":"m-new"}`
	})

	h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1",
		Transcript: true, LogChannelID: "log1", ArchiveCategoryID: "cat1",
	})

	if !uploaded {
		t.Fatal("the upload was never attempted")
	}
	posts := tr.find(http.MethodPost, "/channels/log1/messages")
	if len(posts) != 2 {
		t.Fatalf("log posts = %d, want the upload plus the fallback embed", len(posts))
	}
	fallback := posts[1]
	if strings.HasPrefix(fallback.contentType, "multipart/") {
		t.Fatalf("the fallback must be a plain embed: %q", fallback.contentType)
	}
	if !strings.Contains(fallback.body, uploadFailedNote) {
		t.Fatalf("fallback body %q must say the upload failed", fallback.body)
	}
}

// A history that could not be paged in full is marked, in the reply and in the
// body, so a short transcript is never mistaken for a short conversation.
func TestTicketCloseMarksATruncatedTranscript(t *testing.T) {
	pages := 0
	h, _ := newTicketRPC(t, func(call recordedCall) (int, string) {
		if call.method == http.MethodGet && strings.HasSuffix(call.path, "/messages") {
			pages++
			if pages == 1 {
				return 200, messagePage(1000, discapi.MessagePageMax)
			}
			return 500, `{"message":"Internal Server Error"}`
		}
		return 200, `{"id":"m-new"}`
	})

	reply := h.close(context.Background(), discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1",
		Transcript: true, LogChannelID: "log1", ArchiveCategoryID: "cat1",
	})

	if !reply.Truncated {
		t.Fatalf("reply = %+v, want truncated", reply)
	}
	if !strings.Contains(reply.TranscriptBody, "transcript incomplete") {
		t.Fatalf("transcript %q must carry the truncation line", firstLine(reply.TranscriptBody))
	}
	if reply.MessageCount != discapi.MessagePageMax {
		t.Fatalf("message count = %d, want the partial page kept", reply.MessageCount)
	}
}
