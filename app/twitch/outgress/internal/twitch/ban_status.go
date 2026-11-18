package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"errors"
	"net/http"
	"strings"
	"net/url"
	"time"
)

type BanStatus struct {
	Banned    bool
	ExpiresAt time.Time
}

// BotBanned is a single explicit repair check using the broadcaster's own grant.
// Empty filtered data is definitive absence; provider failures stay unknown.
func (c *Client) BotBanned(ctx context.Context, broadcasterID, botID string) (bool, error) {
	status, err := c.BotBan(ctx, broadcasterID, botID)
	return status.Banned, err
}

func (c *Client) BotBan(ctx context.Context, broadcasterID, botID string) (BanStatus, error) {
	q := url.Values{"broadcaster_id": {broadcasterID}, "user_id": {botID}}
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, getCall("/helix/moderation/banned?"+q.Encode()))
	if err != nil {
		return BanStatus{}, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return BanStatus{}, statusError(res, "bot ban verification")
	}
	var body struct {
		Data []struct {
			UserID    string `json:"user_id"`
			ExpiresAt string `json:"expires_at"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&body); err != nil {
		return BanStatus{}, err
	}
	for _, user := range body.Data {
		if user.UserID == botID {
			return banEntry(user.ExpiresAt)
		}
	}
	return BanStatus{}, nil
}

func MissingScope(err error) bool {
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		return false
	}
	return strings.Contains(se.Body, "Missing scope")
}

func banEntry(expiresAt string) (BanStatus, error) {
	if expiresAt == "" {
		return BanStatus{Banned: true}, nil
	}
	at, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return BanStatus{}, err
	}
	return BanStatus{Banned: true, ExpiresAt: at}, nil
}
