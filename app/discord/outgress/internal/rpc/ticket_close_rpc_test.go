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
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	discapi "ItsBagelBot/internal/discordapi"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
)

const incompleteTranscript = "[transcript incomplete: the oldest messages could not be collected]\n"

func historyGet(call recordedCall) bool {
	return call.method == http.MethodGet && strings.HasSuffix(call.path, "/messages")
}

func closeReq() discordoutgress.TicketCloseRequest {
	return discordoutgress.TicketCloseRequest{
		GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1",
		LogChannelID: "log1", ArchiveCategoryID: "cat1",
	}
}

func wantLogPosts(t *testing.T, tr *scriptedTransport, want int) []recordedCall {
	t.Helper()
	posts := tr.find(http.MethodPost, "/channels/log1/messages")
	if len(posts) != want {
		t.Fatalf("log posts = %d, want %d", len(posts), want)
	}
	return posts
}

func wantChannelPatch(t *testing.T, tr *scriptedTransport) recordedCall {
	t.Helper()
	patches := tr.find(http.MethodPatch, "/channels/c1")
	if len(patches) != 1 {
		t.Fatalf("channel patches = %d, want 1", len(patches))
	}
	return patches[0]
}

func wantContainsAll(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q missing %q", body, want)
		}
	}
}

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

func transcriptOf(high, n int) string {
	var b strings.Builder
	for id := high - n + 1; id <= high; id++ {
		fmt.Fprintf(&b, "[2026-01-02 03:04 UTC] ada: line %d\n", id)
	}
	return b.String()
}

func endlessHistory(call recordedCall) (int, string) {
	if !historyGet(call) {
		return http.StatusOK, `{"id":"m-new"}`
	}
	top := 100000
	if before := beforeCursor(call); before > 0 {
		top = before - 1
	}
	return http.StatusOK, messagePage(top, discapi.MessagePageMax)
}

func beforeCursor(call recordedCall) int {
	query, _ := url.ParseQuery(call.query)
	before, _ := strconv.Atoi(query.Get("before"))
	return before
}

func historyCalls(top, pages int) []string {
	out := []string{"GET /channels/c1/messages?limit=100"}
	for page := 1; page < pages; page++ {
		before := top - page*discapi.MessagePageMax + 1
		out = append(out, "GET /channels/c1/messages?before="+strconv.Itoa(before)+"&limit=100")
	}
	return out
}

func ticketCloseCases() []discordCase {
	refused := answer{status: http.StatusForbidden, body: missingPermissions}
	return []discordCase{{
		name: "deletes the channel and posts a plain summary when no archive is set",
		verb: "ticket.close",
		req: discordoutgress.TicketCloseRequest{
			GuildID: "g1", ChannelID: "c1", ChannelName: "ticket-ada-1", LogChannelID: "log1",
		},
		want:  discordoutgress.TicketCloseReply{},
		calls: []string{"GET /channels/c1", "DELETE /channels/c1", "GET /channels/log1", "POST /channels/log1/messages"},
	}, {
		name:  "posts nothing without a log channel",
		verb:  "ticket.close",
		req:   discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1"},
		want:  discordoutgress.TicketCloseReply{},
		calls: []string{"GET /channels/c1", "DELETE /channels/c1"},
	}, {
		name: "maps a refused archive onto the forbidden code",
		verb: "ticket.close",
		req: discordoutgress.TicketCloseRequest{
			GuildID: "g1", ChannelID: "c1", ChannelName: "t", ArchiveCategoryID: "cat1",
		},
		routes: map[string]answer{"PATCH /channels/c1": refused},
		want: discordoutgress.TicketCloseReply{
			Error: "discord: forbidden: " + missingPermissions, Code: outgressrpc.CodeForbidden,
		},
		calls: []string{"GET /channels/c1", "PATCH /channels/c1"},
	}, {
		name:  "archives an unnamed ticket without renaming it",
		verb:  "ticket.close",
		req:   discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1", ArchiveCategoryID: "cat1"},
		want:  discordoutgress.TicketCloseReply{ArchivedChannelID: "c1"},
		calls: []string{"GET /channels/c1", "PATCH /channels/c1"},
		write: "PATCH /channels/c1",
		body:  `{"parent_id":"cat1","permission_overwrites":[{"id":"g1","type":0,"allow":"0","deny":"1024"}]}`,
	}, {
		name:   "stops paging at the transcript cap and marks it incomplete",
		verb:   "ticket.close",
		req:    discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1", Transcript: true},
		script: endlessHistory,
		want: discordoutgress.TicketCloseReply{
			MessageCount: 2000, Truncated: true, TranscriptBody: incompleteTranscript + transcriptOf(100000, 2000),
		},
		calls: slices.Concat([]string{"GET /channels/c1"}, historyCalls(100000, 20), []string{"DELETE /channels/c1"}),
	}, {
		name:   "closes an empty ticket without a transcript",
		verb:   "ticket.close",
		req:    discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1", Transcript: true},
		routes: map[string]answer{"GET /channels/c1/messages?limit=100": {status: http.StatusOK, body: `[]`}},
		want:   discordoutgress.TicketCloseReply{},
		calls:  []string{"GET /channels/c1", "GET /channels/c1/messages?limit=100", "DELETE /channels/c1"},
	}, {
		name: "keeps embeds and attachment links in the transcript",
		verb: "ticket.close",
		req:  discordoutgress.TicketCloseRequest{GuildID: "g1", ChannelID: "c1", Transcript: true},
		routes: map[string]answer{"GET /channels/c1/messages?limit=100": {status: http.StatusOK, body: `[{
			"id":"5","content":"see attached","timestamp":"2026-01-02T03:04:05+00:00",
			"author":{"id":"u1","username":"ada"},
			"embeds":[{"title":"Order","description":"#42"}],
			"attachments":[{"url":"https://cdn.example/receipt.png","filename":"receipt.png"}]}]`}},
		want: discordoutgress.TicketCloseReply{
			MessageCount: 1,
			TranscriptBody: "[2026-01-02 03:04 UTC] ada: see attached\n" +
				"    [embed] Order: #42\n" +
				"    [attachment] https://cdn.example/receipt.png\n",
		},
		calls: []string{"GET /channels/c1", "GET /channels/c1/messages?limit=100", "DELETE /channels/c1"},
	}}
}

func TestTicketCloseDisposesTheChannelAndAnswers(t *testing.T) {
	for _, tc := range ticketCloseCases() {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected(t), tc.exchange(t, serveTickets))
		})
	}
}

type transcriptUpload struct {
	Payload uploadPayload
	File    string
}

type uploadPayload struct {
	Attachments []uploadAttachment `json:"attachments"`
	Embeds      []uploadEmbed      `json:"embeds"`
}

type uploadAttachment struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
}

type uploadEmbed struct {
	Title string `json:"title"`
}

func uploadOf(t *testing.T, call recordedCall) transcriptUpload {
	t.Helper()
	_, params, err := mime.ParseMediaType(call.contentType)
	require.NoError(t, err)
	form, err := multipart.NewReader(strings.NewReader(call.body), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	var payload uploadPayload
	require.NoError(t, json.Unmarshal([]byte(form.Value["payload_json"][0]), &payload))
	file, err := form.File["files[0]"][0].Open()
	require.NoError(t, err)
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	return transcriptUpload{Payload: payload, File: string(data)}
}

func TestTicketClosePagesHistoryUploadsAndArchives(t *testing.T) {
	req := closeReq()
	req.OpenerID = "u1"
	req.Transcript = true
	req.StaffRoleIDs = []string{"rmod", ""}
	req.Summary = discordoutgress.TicketCloseSummary{Opener: "<@u1>", Closer: "Mod"}
	transcript := transcriptOf(1000, discapi.MessagePageMax+3)
	tc := discordCase{
		verb: "ticket.close",
		req:  req,
		routes: map[string]answer{
			"GET /channels/c1/messages?limit=100":            {status: http.StatusOK, body: messagePage(1000, discapi.MessagePageMax)},
			"GET /channels/c1/messages?before=901&limit=100": {status: http.StatusOK, body: messagePage(900, 3)},
		},
		want: discordoutgress.TicketCloseReply{
			MessageCount: discapi.MessagePageMax + 3, TranscriptBody: transcript, ArchivedChannelID: "c1",
		},
		calls: []string{
			"GET /channels/c1", "GET /channels/c1/messages?limit=100", "GET /channels/c1/messages?before=901&limit=100",
			"PATCH /channels/c1", "GET /channels/log1", "POST /channels/log1/messages multipart",
		},
		write: "PATCH /channels/c1",
		body: `{"name":"closed-ticket-ada-1","parent_id":"cat1","permission_overwrites":[` +
			`{"id":"g1","type":0,"allow":"0","deny":"1024"},{"id":"u1","type":1,"allow":"0","deny":"1024"},` +
			`{"id":"rmod","type":0,"allow":"66560","deny":"0"}]}`,
	}

	got, tr := tc.exchangeWith(t, serveTickets)

	require.Equal(t, tc.expected(t), got)
	require.Equal(t, transcriptUpload{
		Payload: uploadPayload{
			Attachments: []uploadAttachment{{ID: 0, Filename: "ticket-ada-1.txt"}},
			Embeds:      []uploadEmbed{{Title: "Ticket closed"}},
		},
		File: transcript,
	}, uploadOf(t, tr.find(http.MethodPost, "/channels/log1/messages")[0]))
}

func TestArchiveOverwritesCarryBothHalves(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	req := closeReq()
	req.LogChannelID = ""
	req.OpenerID = "u1"
	req.StaffRoleIDs = []string{"rmod"}

	h.close(context.Background(), req)

	body := wantChannelPatch(t, tr).body
	wantContainsAll(t, body,
		`{"id":"g1","type":0,"allow":"0","deny":"1024"}`,
		`{"id":"u1","type":1,"allow":"0","deny":"1024"}`,
		`{"id":"rmod","type":0,"allow":"66560","deny":"0"}`,
	)
	if strings.Contains(body, `"allow":""`) {
		t.Fatalf("patch body carries an empty allow: %q", body)
	}
	if strings.Contains(body, `"deny":""`) {
		t.Fatalf("patch body carries an empty deny: %q", body)
	}
}

func TestTicketCloseDisposesBeforePostingTheSummary(t *testing.T) {
	h, tr := newTicketRPC(t, nil)

	h.close(context.Background(), closeReq())

	dispose := tr.indexOf(http.MethodPatch, "/channels/c1")
	summary := tr.indexOf(http.MethodPost, "/channels/log1/messages")
	if dispose < 0 {
		t.Fatalf("the channel was never archived: %+v", tr.calls)
	}
	if summary < 0 {
		t.Fatalf("the summary was never posted: %+v", tr.calls)
	}
	if dispose > summary {
		t.Fatal("the channel must be archived before the summary posts")
	}
}

func TestTicketCloseSummaryPostsOncePerTicket(t *testing.T) {
	h, tr := newTicketRPC(t, nil)
	h.memo = newOnceMemo()
	req := closeReq()
	req.TicketID = 42

	h.close(context.Background(), req)
	h.close(context.Background(), req)

	wantLogPosts(t, tr, 1)
}

func TestTicketCloseWithoutARowIDAlwaysPosts(t *testing.T) {
	h, tr := newTicketRPC(t, nil)
	h.memo = newOnceMemo()
	req := closeReq()
	req.ChannelName = ""
	req.TicketID = 0

	h.close(context.Background(), req)
	h.close(context.Background(), req)

	wantLogPosts(t, tr, 2)
}

func TestTicketCloseFallsBackToAnEmbedWhenTheUploadFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{name: "fallback succeeds", status: http.StatusOK},
		{name: "fallback fails", status: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
				if historyGet(call) {
					return 200, messagePage(1000, 2)
				}
				if strings.HasPrefix(call.contentType, "multipart/") {
					return 400, `{"message":"Request entity too large"}`
				}
				if call.method == http.MethodPost && call.path == "/channels/log1/messages" {
					return tc.status, `{"id":"m-new"}`
				}
				return 200, `{"id":"m-new"}`
			})

			req := closeReq()
			req.Transcript = true
			req.ChannelName = ""

			reply := h.close(context.Background(), req)

			require.Empty(t, reply.Error, "summary delivery must not fail ticket close")
			posts := wantLogPosts(t, tr, 2)
			require.Contains(t, posts[0].body, `filename="transcript.txt"`)
			fallback := posts[1]
			require.False(t, strings.HasPrefix(fallback.contentType, "multipart/"), "the fallback must be a plain embed")
			require.Contains(t, fallback.body, uploadFailedNote)
		})
	}
}

func TestTicketCloseMarksATruncatedTranscript(t *testing.T) {
	for _, tc := range []struct {
		name              string
		archiveCategoryID string
		wantCode          domainrpc.Code
	}{
		{name: "archive succeeds", archiveCategoryID: "cat1", wantCode: outgressrpc.CodeOK},
		{name: "deletion fails", wantCode: outgressrpc.CodeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstPage := true
			h, tr := newTicketRPC(t, func(call recordedCall) (int, string) {
				if historyGet(call) && firstPage {
					firstPage = false
					return 200, messagePage(1000, discapi.MessagePageMax)
				}
				if historyGet(call) || call.method == http.MethodDelete {
					return 500, `{"message":"Internal Server Error"}`
				}
				return 200, `{"id":"m-new"}`
			})

			req := closeReq()
			req.Transcript = true
			req.ArchiveCategoryID = tc.archiveCategoryID

			reply := h.close(context.Background(), req)

			historyIdx := tr.indexOf(http.MethodGet, "/channels/c1/messages")
			disposeIdx := tr.indexOf(http.MethodDelete, "/channels/c1")
			if disposeIdx < 0 {
				disposeIdx = tr.indexOf(http.MethodPatch, "/channels/c1")
			}
			require.Greater(t, disposeIdx, historyIdx, "history must be read before disposing the channel")
			require.Equal(t, tc.wantCode, reply.Code)
			require.Equal(t, tc.wantCode != outgressrpc.CodeOK, reply.Error != "")
			require.True(t, reply.Truncated)
			require.Equal(t, discapi.MessagePageMax, reply.MessageCount)
			require.Contains(t, reply.TranscriptBody, "transcript incomplete")
			require.Contains(t, reply.TranscriptBody, "line 1000")
		})
	}
}
