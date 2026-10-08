// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi_test

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	api "ItsBagelBot/internal/discordapi"
	domain "ItsBagelBot/internal/domain/discord"
)

func TestSendFileUploadsPayloadJSONAndTheFile(t *testing.T) {
	client, discord := fakeDiscord(http.StatusOK, `{"id":"m1"}`)
	embed := domain.Embed{Title: "Ticket closed"}
	transcript := "[2026-01-01 00:00 UTC] ada: hi\n"

	msg, err := client.SendFile(context.Background(), api.FileUpload{
		ChannelID: "c1", Filename: "ticket-ada-1.txt", Data: []byte(transcript), Content: "closed", Embed: &embed,
	})

	require.NoError(t, err)
	assert.Equal(t, api.Message{ChannelID: "c1", ID: "m1"}, msg)
	assert.Equal(t, sentRequest{Method: http.MethodPost, URI: "/channels/c1/messages"},
		sentRequest{Method: discord.sent.Method, URI: discord.sent.URI})
	assert.Equal(t, map[string]string{
		"payload_json": `{"attachments":[{"filename":"ticket-ada-1.txt","id":0}],"content":"closed","embeds":[{"title":"Ticket closed"}]}`,
		"files[0]":     transcript,
	}, multipartParts(t, discord))
}

func multipartParts(t *testing.T, d *discord) map[string]string {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(d.header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(strings.NewReader(d.sent.Body), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	out := map[string]string{}
	for name, values := range form.Value {
		out[name] = canonical([]byte(values[0]))
	}
	for name, files := range form.File {
		out[name] = readPart(t, files[0])
	}
	return out
}

func readPart(t *testing.T, header *multipart.FileHeader) string {
	t.Helper()
	f, err := header.Open()
	require.NoError(t, err)
	defer f.Close()
	raw, err := io.ReadAll(f)
	require.NoError(t, err)
	return string(raw)
}
