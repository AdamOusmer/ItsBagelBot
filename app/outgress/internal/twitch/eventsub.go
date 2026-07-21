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
//   - stream.online / stream.offline carry only broadcaster_user_id and are
//     authorized by the conduit app token alone (no user scope), so they cannot
//     401. They drive the live-event subsystem (go-live cache prewarm and the
//     mod-status re-verify), so every channel must carry them.
func ChannelSubscriptions(broadcasterID, botID string) []SubSpec {
	specs := make([]SubSpec, 0, 10)

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
		// Condition keys off the receiving channel: a raid is "to" this broadcaster,
		// not "from" them, so to_broadcaster_user_id is what makes this channel's
		// raid handlers (shoutout, alerts) actually fire.
		SubSpec{"channel.raid", "1", map[string]string{"to_broadcaster_user_id": broadcasterID}},
		SubSpec{"channel.update", "2", map[string]string{"broadcaster_user_id": broadcasterID}},
		// Authorized by the conduit app token alone (broadcaster_user_id only,
		// no user scope, no 401 risk). These deliver go-live/go-offline, which
		// the live-event subsystem depends on.
		SubSpec{"stream.online", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		SubSpec{"stream.offline", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
	)
}

// ChannelOptionalSubscriptions lists subscriptions that only some channels can
// carry, so a create failure must not fail the whole enroll:
//
//   - channel.channel_points_custom_reward_redemption.add drives the channel
//     points module. It is authorized by the broadcaster's channel:read:redemptions
//     grant (part of the channel:manage:redemptions consent the dashboard adds).
//     A non-affiliate has no channel points, so Twitch answers 403 — expected, not
//     a fault. It also 401s for a broadcaster whose stored grant predates the
//     scope, until they re-consent. Both are permanent and tolerated by the
//     caller; the mandatory set is unaffected.
//
//   - channel.ad_break.begin drives the ads chat alert. It is authorized by the
//     broadcaster's channel:read:ads grant, which only consents issued after the
//     scope was added to the dashboard login carry, so it 401s for older grants
//     until the broadcaster re-consents. Non-affiliates never run ads, but the
//     subscription itself is accepted; it simply never fires.
func ChannelOptionalSubscriptions(broadcasterID string) []SubSpec {
	return []SubSpec{
		{"channel.channel_points_custom_reward_redemption.add", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
		{"channel.ad_break.begin", "1", map[string]string{"broadcaster_user_id": broadcasterID}},
	}
}

// ClientSubscriptions lists the client-scoped subscriptions the conduit
// carries exactly once (not per channel): user.authorization.grant and
// user.authorization.revoke, keyed on our client id and authorized by the app
// token alone. They are how Twitch tells us a broadcaster's consent appeared
// or died, which drives the revoked enrollment state and the re-enroll that
// follows a re-consent. Ensured idempotently at worker startup.
func ClientSubscriptions(clientID string) []SubSpec {
	return []SubSpec{
		{"user.authorization.grant", "1", map[string]string{"client_id": clientID}},
		{"user.authorization.revoke", "1", map[string]string{"client_id": clientID}},
	}
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
//
// Condition.BroadcasterUserID matters because the list query (?user_id=<id>)
// matches that id in ANY condition field, not just broadcaster_user_id. A
// channel.chat.message sub carries the bot as condition.user_id, so listing by
// the bot's id returns EVERY channel's chat sub. Deletion must therefore filter
// on broadcaster_user_id, or reconnecting the bot account would wipe every
// channel's chat subscription.
type EventSubEntry struct {
	ID        string `json:"id"`
	Transport struct {
		Method    string `json:"method"`
		ConduitID string `json:"conduit_id"`
	} `json:"transport"`
	Condition struct {
		BroadcasterUserID string `json:"broadcaster_user_id"`
	} `json:"condition"`
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
