// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoHClassifications(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantBlocked bool
		wantErr     bool
	}{
		{"reads a sinkhole to 0.0.0.0 as blocked", http.StatusOK, `{"Status":0,"Answer":[{"name":"evil.example","type":1,"TTL":300,"data":"0.0.0.0"}]}`, true, false},
		{"reads a sinkhole to :: as blocked", http.StatusOK, `{"Status":0,"Answer":[{"type":28,"data":"::"}]}`, true, false},
		{"reads a normal address as clean", http.StatusOK, `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`, false, false},
		{"reads nxdomain as clean, not an error", http.StatusOK, `{"Status":3}`, false, false},
		{"surfaces a resolver error", http.StatusServiceUnavailable, "upstream sad", false, true},
		{"surfaces a garbage body", http.StatusOK, "<html>not json</html>", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			blocked, err := NewDoH(srv.URL, nil).Blocked(context.Background(), "host.example")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantBlocked, blocked)
		})
	}
}

func TestDoHSurfacesATransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	_, err := NewDoH(srv.URL, nil).Blocked(context.Background(), "host.example")

	require.Error(t, err)
}
