// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	domain "ItsBagelBot/internal/domain/discord"
)

// recordingWithHeader is recording plus the request's Content-Type, which is
// the whole point of the multipart path: the boundary lives in the header and
// a body written without it is unparseable.
func recordingWithHeader(t *testing.T, status int, reply string) (*Client, *capture, *string) {
	t.Helper()
	client, got := recording(t, status, reply)
	contentType := new(string)
	inner := client.http.Transport
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*contentType = r.Header.Get("Content-Type")
		return inner.RoundTrip(r)
	}))
	return client, got, contentType
}

func TestSendFileSendsPayloadJSONAndFilesZero(t *testing.T) {
	client, got, contentType := recordingWithHeader(t, 200, `{"id":"m1"}`)

	embed := domain.Embed{Title: "Ticket closed"}
	msg, err := client.SendFile(context.Background(), FileUpload{
		ChannelID: "c1", Filename: "ticket-ada-1.txt", Data: []byte("[2026-01-01 00:00 UTC] ada: hi\n"),
		Content: "closed", Embed: &embed,
	})
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if msg.ID != "m1" || msg.ChannelID != "c1" {
		t.Fatalf("message = %+v", msg)
	}
	wantRequest(t, got, http.MethodPost, "/channels/c1/messages")

	parts := parseMultipart(t, *contentType, got.body)
	wantPayloadJSON(t, parts)
	// The attachments entry's id must match the files[N] index, or Discord
	// accepts the message and silently drops the file.
	if parts["files[0]"] != "[2026-01-01 00:00 UTC] ada: hi\n" {
		t.Fatalf("files[0] = %q", parts["files[0]"])
	}
}

// wantPayloadJSON checks the JSON part carries the message body Discord binds
// the attachment to: the attachments entry, its index-matching id and
// filename, plus the content and embed posted alongside it.
func wantPayloadJSON(t *testing.T, parts map[string]string) {
	t.Helper()
	payload, ok := parts["payload_json"]
	if !ok {
		t.Fatalf("no payload_json part in %v", keys(parts))
	}
	for _, want := range []string{`"attachments"`, `"id":0`, `"filename":"ticket-ada-1.txt"`, `"content":"closed"`, `"Ticket closed"`} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload_json %q missing %q", payload, want)
		}
	}
}

func TestSendFileClassifiesForbidden(t *testing.T) {
	client, _, _ := recordingWithHeader(t, 403, `{"message":"Missing Permissions"}`)

	_, err := client.SendFile(context.Background(), FileUpload{ChannelID: "c1", Filename: "t.txt", Data: []byte("x")})

	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden (the bot needs ATTACH_FILES)", err)
	}
}

func parseMultipart(t *testing.T, contentType, body string) map[string]string {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("Content-Type %q: %v", contentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("media type = %q, want multipart/form-data", mediaType)
	}
	reader := multipart.NewReader(strings.NewReader(body), params["boundary"])
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("read form: %v", err)
	}
	out := map[string]string{}
	for name, values := range form.Value {
		out[name] = values[0]
	}
	for name, files := range form.File {
		f, err := files[0].Open()
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		raw, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		_ = f.Close()
		out[name] = string(raw)
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
