// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeTokenHTTP swaps the package-level tokenHTTP client for the duration of
// the test, so postToken's request to id.twitch.tv never leaves the process.
// Restored automatically via t.Cleanup.
func fakeTokenHTTP(t *testing.T, handler roundTripFunc) {
	t.Helper()
	orig := tokenHTTP
	tokenHTTP = &http.Client{Transport: handler}
	t.Cleanup(func() { tokenHTTP = orig })
}

// fakeOAuthResponse builds the http.Response postToken expects from a
// successful grant.
func fakeOAuthResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
