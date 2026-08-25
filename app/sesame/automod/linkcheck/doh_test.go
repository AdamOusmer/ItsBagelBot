// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func dohServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestDoHClassifications(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		blocked bool
		wantErr bool
	}{
		{
			name:    "sinkholed to 0.0.0.0",
			status:  http.StatusOK,
			body:    `{"Status":0,"Answer":[{"name":"evil.example","type":1,"TTL":300,"data":"0.0.0.0"}]}`,
			blocked: true,
		},
		{
			name:    "sinkholed to :: AAAA",
			status:  http.StatusOK,
			body:    `{"Status":0,"Answer":[{"type":28,"data":"::"}]}`,
			blocked: true,
		},
		{
			name:    "normal address clean",
			status:  http.StatusOK,
			body:    `{"Status":0,"Answer":[{"type":1,"data":"93.184.216.34"}]}`,
			blocked: false,
		},
		{
			name:    "nxdomain reads clean, not error",
			status:  http.StatusOK,
			body:    `{"Status":3}`,
			blocked: false,
		},
		{
			name:    "resolver error surfaces as error",
			status:  http.StatusServiceUnavailable,
			body:    "upstream sad",
			wantErr: true,
		},
		{
			name:    "garbage body surfaces as error",
			status:  http.StatusOK,
			body:    "<html>not json</html>",
			wantErr: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDoH(dohServer(t, tt.status, tt.body).URL, nil)
			blocked, err := d.Blocked(context.Background(), "host.example")
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if blocked != tt.blocked {
				t.Fatalf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}
