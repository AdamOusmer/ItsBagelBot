// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func fakeTokenHTTP(t *testing.T, handler roundTripFunc) {
	t.Helper()
	orig := tokenHTTP
	tokenHTTP = &http.Client{Transport: handler}
	t.Cleanup(func() { tokenHTTP = orig })
}

func fakeOAuthResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
