// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	api "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/pkg/codec"
)

const botToken = "bot-token"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sentRequest struct {
	Method string
	URI    string
	Body   string
}

type discord struct {
	status int
	reply  string
	sent   sentRequest
	header http.Header
}

func fakeDiscord(status int, reply string) (*api.Client, *discord) {
	d := &discord{status: status, reply: reply}
	client := api.NewClient(botToken)
	client.SetTransport(roundTripFunc(d.roundTrip))
	return client, d
}

func (d *discord) roundTrip(r *http.Request) (*http.Response, error) {
	d.header = r.Header.Clone()
	d.sent = sentRequest{Method: r.Method, URI: strings.TrimPrefix(r.URL.RequestURI(), "/api/v10"), Body: bodyOf(r)}
	rec := httptest.NewRecorder()
	rec.Code = d.status
	if r.Header.Get("Authorization") != "Bot "+botToken {
		rec.Code = http.StatusUnauthorized
	}
	rec.Body.WriteString(d.reply)
	return rec.Result(), nil
}

func bodyOf(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	raw, _ := io.ReadAll(r.Body)
	return canonical(raw)
}

func canonical(raw []byte) string {
	var v any
	if codec.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := codec.Marshal(v)
	return string(out)
}
