package twitch

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestBotBanRepairIsFilteredAndNeverConflatesProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		body    string
		status  int
		want    BanStatus
		wantErr bool
	}{
		{`{"data":[]}`, 200, BanStatus{}, false},
		{`{"data":[{"user_id":"bot","expires_at":""}]}`, 200, BanStatus{Banned: true}, false},
		{`{"data":[{"user_id":"bot","expires_at":"2026-11-17T12:00:00Z"}]}`, 200, BanStatus{Banned: true, ExpiresAt: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}, false},
		{`{"data":[{"user_id":"bot","expires_at":"soon"}]}`, 200, BanStatus{}, true},
		{`{"message":"missing scope"}`, 403, BanStatus{}, true},
		{`{"message":"outage"}`, 503, BanStatus{}, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			calls := 0
			c := helixClient(nil, nil, func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "owner", req.URL.Query().Get("broadcaster_id"))
				require.Equal(t, "bot", req.URL.Query().Get("user_id"))
				require.Equal(t, "Bearer owner-token", req.Header.Get("Authorization"))
				return respond(tc.status, tc.body), nil
			})
			c.broadcasters = NewBroadcasterTokens(func(string) *Source { return NewStaticTokenSource("owner-token") })
			status, err := c.BotBan(context.Background(), "owner", "bot")
			require.Equal(t, tc.want, status)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, 1, calls)
		})
	}
}
