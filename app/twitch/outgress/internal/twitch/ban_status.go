package twitch

import (
	"ItsBagelBot/pkg/codec"
	"context"
	"net/http"
	"net/url"
)

// BotBanned is a single explicit repair check using the broadcaster's own grant.
// Empty filtered data is definitive absence; provider failures stay unknown.
func (c *Client) BotBanned(ctx context.Context, broadcasterID, botID string) (bool, error) {
	q := url.Values{"broadcaster_id": {broadcasterID}, "user_id": {botID}}
	res, err := c.ExecuteAs(ctx, IdentityBroadcaster, broadcasterID, getCall("/helix/moderation/banned?"+q.Encode()))
	if err != nil {
		return false, err
	}
	defer drain(res)
	if res.StatusCode != http.StatusOK {
		return false, statusError(res, "bot ban verification")
	}
	var body struct {
		Data []struct {
			UserID string `json:"user_id"`
		} `json:"data"`
	}
	if err := codec.NewDecoder(res.Body).Decode(&body); err != nil {
		return false, err
	}
	for _, user := range body.Data {
		if user.UserID == botID {
			return true, nil
		}
	}
	return false, nil
}
