package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// SubSpec describes one EventSub subscription the bot needs on a channel.
type SubSpec struct {
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	Condition map[string]string `json:"condition"`
}

// ChannelSubscriptions lists everything the receive toggle turns on for one
// channel, split by the identity that authorizes each subscription:
//
//   - the bot account receives chat. channel.chat.message is read in the bot's
//     user context (user_id = botID), authorized by the bot account's
//     user:read:chat / user:bot grant plus the broadcaster's channel:bot grant.
//     It is omitted when botID is empty, since the subscription cannot be built
//     without the bot's user id.
//   - broadcaster rights cover the channel events the broadcaster's onboarding
//     consent unlocks: subs, gift subs, resub messages, cheers, follows, and
//     title/category changes (channel.update).
func ChannelSubscriptions(broadcasterID, botID string) []SubSpec {
	specs := make([]SubSpec, 0, 7)

	if botID != "" {
		specs = append(specs,
			SubSpec{"channel.chat.message", "1", map[string]string{"broadcaster_user_id": broadcasterID, "user_id": botID}},
		)
	}

	return append(specs,
		SubSpec{"channel.subscribe", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.subscription.gift", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.subscription.message", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.cheer", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.follow", "2", map[string]string{"broadcaster_user_id": broadcasterID, "moderator_user_id": broadcasterID}},
		SubSpec{"channel.update", "2", map[string]string{"broadcaster_user_id": broadcasterID}},
	)
}

// CreateEventSub creates one subscription on the Conduit under the app
// token. An already-existing subscription (409) is success, so re-running a
// partially applied job converges instead of failing.
func (c *Client) CreateEventSub(ctx context.Context, spec SubSpec, conduitID string) error {

	body, _ := json.Marshal(map[string]any{
		"type":      spec.Type,
		"version":   spec.Version,
		"condition": spec.Condition,
		"transport": map[string]string{
			"method":     "conduit",
			"conduit_id": conduitID,
		},
	})

	res, err := c.Do(ctx, http.MethodPost, "/helix/eventsub/subscriptions", body)
	if err != nil {
		return err
	}
	defer drain(res)

	if res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusConflict {
		return nil
	}
	return statusError(res, "eventsub create")
}

// EventSubEntry is one row of an EventSub listing; only what deletion needs.
type EventSubEntry struct {
	ID        string `json:"id"`
	Transport struct {
		Method    string `json:"method"`
		ConduitID string `json:"conduit_id"`
	} `json:"transport"`
}

// ListEventSubs returns the subscriptions whose condition references userID,
// one page at a time.
func (c *Client) ListEventSubs(ctx context.Context, userID, cursor string) ([]EventSubEntry, string, error) {

	endpoint := "/helix/eventsub/subscriptions?user_id=" + url.QueryEscape(userID)
	if cursor != "" {
		endpoint += "&after=" + url.QueryEscape(cursor)
	}

	res, err := c.Do(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	defer drain(res)

	if res.StatusCode != http.StatusOK {
		return nil, "", statusError(res, "eventsub list")
	}

	var page struct {
		Data       []EventSubEntry `json:"data"`
		Pagination struct {
			Cursor string `json:"cursor"`
		} `json:"pagination"`
	}
	if err := json.NewDecoder(res.Body).Decode(&page); err != nil {
		return nil, "", err
	}
	return page.Data, page.Pagination.Cursor, nil
}

// DeleteEventSub removes one subscription. A 404 means someone else already
// removed it, which is the state we wanted.
func (c *Client) DeleteEventSub(ctx context.Context, id string) error {

	res, err := c.Do(ctx, http.MethodDelete, "/helix/eventsub/subscriptions?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	defer drain(res)

	if res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotFound {
		return nil
	}
	return statusError(res, "eventsub delete")
}

// StatusError carries the HTTP status so callers can split retryable from
// permanent failures.
type StatusError struct {
	Status int
	Op     string
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Op, e.Status, e.Body)
}

func statusError(res *http.Response, op string) error {
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
	return &StatusError{Status: res.StatusCode, Op: op, Body: string(body)}
}
