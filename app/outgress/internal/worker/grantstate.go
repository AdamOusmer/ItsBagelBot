package worker

import (
	"context"
	"net/http"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/rpc/manage"

	"go.uber.org/zap"
)

// grantRegistry is the slice of the channel registry the grant marker needs.
// Narrow on purpose: it exists so the marker's transition logic is testable
// without standing up Valkey, and *channels.Registry satisfies it as-is.
type grantRegistry interface {
	Get(ctx context.Context, broadcasterID string) (manage.Channel, bool, error)
	SetGrantState(ctx context.Context, broadcasterID string, state manage.GrantState) error
}

// callTwitch is ExecuteAs plus grant bookkeeping. Every broadcaster-identity
// call goes through here so a dead grant is learned from traffic the bot was
// going to send anyway: no probe, no scheduled refresh, and therefore no way
// for this path to burn a refresh token or manufacture the very breakage it
// reports.
func (w *Worker) callTwitch(ctx context.Context, id twitch.Identity, broadcasterID string, call twitch.HelixCall) (*http.Response, error) {
	res, err := w.twitch.ExecuteAs(ctx, id, broadcasterID, call)
	w.noteGrantHealth(ctx, id, broadcasterID, err)
	return res, err
}

// noteGrantHealth records what the call just proved about the broadcaster's own
// grant. Best-effort throughout: this is a notification signal, and it must
// never change whether the caller's job succeeds.
func (w *Worker) noteGrantHealth(ctx context.Context, id twitch.Identity, broadcasterID string, err error) {
	if !w.grantTrackable(id, broadcasterID) {
		return
	}
	if twitch.GrantDead(err) {
		w.setGrantState(ctx, broadcasterID, manage.GrantDead)
		return
	}
	if err == nil {
		w.setGrantState(ctx, broadcasterID, manage.GrantUnknown)
	}
}

// grantTrackable gates the marker to calls that actually say something about
// the broadcaster's own grant.
//
// The identity check is load-bearing, not defensive. execute.go carries app
// chat sends and bot moderation actions under real broadcaster ids, and
// postToken is shared by the app, bot and broadcaster grants. Without this
// gate, one client-credentials failure would mark every broadcaster whose
// traffic happened to pass during it, and chat being the highest-volume type,
// that is the entire enrolled fleet within seconds.
func (w *Worker) grantTrackable(id twitch.Identity, broadcasterID string) bool {
	return id == twitch.IdentityBroadcaster && broadcasterID != "" && w.grants != nil
}

// currentGrantState reads the channel's recorded grant health. The second
// return is false when the channel is unreadable or not registered, which are
// the two cases where the marker must stay out of the way rather than conjure
// a registry entry.
func (w *Worker) currentGrantState(ctx context.Context, broadcasterID string) (manage.GrantState, bool) {
	ch, found, err := w.grants.Get(ctx, broadcasterID)
	if err != nil || !found {
		return manage.GrantUnknown, false
	}
	return ch.GrantState, true
}

// setGrantState writes only on an observed transition. The read is served by
// the registry's own cache, so the common case (a healthy channel making a
// successful call) costs nothing.
func (w *Worker) setGrantState(ctx context.Context, broadcasterID string, state manage.GrantState) {
	current, ok := w.currentGrantState(ctx, broadcasterID)
	if !ok {
		return
	}
	if current == state {
		return
	}

	if err := w.grants.SetGrantState(ctx, broadcasterID, state); err != nil {
		w.log.Warn("grant state write failed",
			zap.String("broadcaster_id", broadcasterID),
			zap.String("grant_state", string(state)),
			zap.Error(err))
		return
	}

	w.log.Info("grant state changed",
		zap.String("broadcaster_id", broadcasterID),
		zap.String("from", string(current)),
		zap.String("to", string(state)))

	w.notifyGrantDead(ctx, broadcasterID, state)
}

// clearGrantDead drops the marker on re-consent, using a channel the caller
// already read. This is the escape hatch that does not depend on the enroll
// lifecycle: user.authorization.grant fires on every re-consent, so a streamer
// who reconnects always reaches it.
func (w *Worker) clearGrantDead(ctx context.Context, broadcasterID string, ch manage.Channel) {
	if w.grants == nil || ch.GrantState != manage.GrantDead {
		return
	}
	if err := w.grants.SetGrantState(ctx, broadcasterID, manage.GrantUnknown); err != nil {
		w.log.Warn("clearing grant state failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	w.log.Info("grant restored by re-consent", zap.String("broadcaster_id", broadcasterID))
}

// notifyGrantDead raises the dashboard bell the moment the grant is first seen
// dead, rather than waiting for the next go-live. The go-live beacon still
// posts the chat line; this is the surface that reaches a streamer who is
// offline, or mid-stream when the grant dies. The notifications service dedupes
// on request id per day, so a repeat within the day collapses server-side.
func (w *Worker) notifyGrantDead(ctx context.Context, broadcasterID string, state manage.GrantState) {
	if state != manage.GrantDead || w.reauth == nil {
		return
	}
	w.reauth.Notify(ctx, broadcasterID, noticeGrantDead)
}
