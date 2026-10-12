package twitch

import (
	"context"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestBotBanRepairIsFilteredAndNeverConflatesProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		body            string
		status          int
		banned, wantErr bool
	}{
		{`{"data":[]}`, 200, false, false},
		{`{"data":[{"user_id":"bot"}]}`, 200, true, false},
		{`{"message":"missing scope"}`, 403, false, true},
		{`{"message":"outage"}`, 503, false, true},
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
			banned, err := c.BotBanned(context.Background(), "owner", "bot")
			require.Equal(t, tc.banned, banned)
			require.Equal(t, tc.wantErr, err != nil)
			require.Equal(t, 1, calls)
		})
	}
}
