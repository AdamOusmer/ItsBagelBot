// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkcheck

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func redirectServer(t *testing.T, location string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if location == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, location, http.StatusMovedPermanently)
	}))
	t.Cleanup(s.Close)
	return s
}

func tok(url string) string { return trimLinkToken(url) }

type dnsMap map[string]string

func clientFor(t *testing.T, m dnsMap) *http.Client {
	t.Helper()
	return &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				real, ok := m[strings.SplitN(addr, ":", 2)[0]]
				if !ok {
					return nil, fmt.Errorf("fake DNS: %q not mapped", addr)
				}
				var d net.Dialer
				return d.DialContext(ctx, network, real)
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func expanderVia(t *testing.T, host string, srv *httptest.Server) *Expander {
	t.Helper()
	return newExpanderScheme(clientFor(t, dnsMap{host: tok(srv.URL)}), []string{host}, "http")
}

func TestExpanderDestination(t *testing.T) {
	tests := []struct {
		name    string
		expand  func(t *testing.T) *Expander
		token   string
		want    string
		wantErr bool
	}{
		{
			name: "follows shorteners until the first non-shortener host",
			expand: func(t *testing.T) *Expander {
				dest := redirectServer(t, "")
				mid := redirectServer(t, "http://sdest.test/final")
				head := redirectServer(t, "http://smid.test/m")
				return newExpanderScheme(
					clientFor(t, dnsMap{"shead.test": tok(head.URL), "smid.test": tok(mid.URL), "sdest.test": tok(dest.URL)}),
					[]string{"shead.test", "smid.test"}, "http")
			},
			token: "shead.test/abc", want: "sdest.test",
		},
		{
			name:   "leaves a non-shortener input uncontacted",
			expand: func(*testing.T) *Expander { return newExpanderScheme(&http.Client{}, []string{"bit.ly"}, "http") },
			token:  "plain.example/x", want: "plain.example",
		},
		{
			name:   "returns the shortener itself behind an interstitial",
			expand: func(t *testing.T) *Expander { return expanderVia(t, "swall.test", redirectServer(t, "")) },
			token:  "swall.test/xyz", want: "swall.test",
		},
		{
			name:   "errors on a redirect loop",
			expand: func(t *testing.T) *Expander { return expanderVia(t, "sloop.test", redirectServer(t, "/self")) },
			token:  "sloop.test/start", wantErr: true,
		},
		{
			name: "refuses a non-http redirect scheme",
			expand: func(t *testing.T) *Expander {
				return expanderVia(t, "sscheme.test", redirectServer(t, "javascript:alert(1)"))
			},
			token: "sscheme.test/x", wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.expand(t).Destination(context.Background(), tt.token)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultExpanderRefusesInternalAddresses(t *testing.T) {
	tests := []struct{ ip, token string }{
		{"127.0.0.1", "127.0.0.1/x"},
		{"10.1.2.3", "10.1.2.3/x"},
		{"192.168.0.20", "192.168.0.20/x"},
		{"172.16.9.9", "172.16.9.9/x"},
		{"100.64.0.9", "100.64.0.9/x"},
		{"169.254.169.254", "169.254.169.254/x"},
		{"224.0.0.1", "224.0.0.1/x"},
		{"0.0.0.0", "0.0.0.0/x"},
		{"fd00::5", "[fd00::5]/x"},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			_, err := NewExpander(nil, []string{tt.ip}).Destination(context.Background(), tt.token)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "dial guard")
		})
	}
}
