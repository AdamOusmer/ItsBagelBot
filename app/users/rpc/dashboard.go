package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/users/ent/tokens"
	"ItsBagelBot/app/users/repository"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
)

// dashboardTimeout bounds each dashboard RPC handler's repo work.
const dashboardTimeout = 3 * time.Second

type dashboardRPC struct {
	repo               *repository.Users
	nc                 *nats.Conn
	invalidationPrefix string
	log                *zap.Logger
}

func SubscribeDashboard(w Wiring, prefix, invalidationPrefix string) error {
	d := &dashboardRPC{
		repo:               w.Repo,
		nc:                 w.NC,
		invalidationPrefix: invalidationPrefix,
		log:                w.Log,
	}

	verbs := map[string]func(context.Context, *nats.Msg){
		"upsert_user":   d.handleUpsertUser,
		"grant_save":    d.handleGrantSave,
		"grant_has":     d.handleGrantHas,
		"active_set":    d.handleActiveSet,
		"active_get":    d.handleActiveGet,
		"status_get":    d.handleStatusGet,
		"state_get":     d.handleStateGet,
		"onboarded_set": d.handleOnboardedSet,
		"locale_set":    d.handleLocaleSet,
		"delete_self":   d.handleDeleteSelf,
	}
	for verb, fn := range verbs {
		subject := prefix + "." + verb
		fn := fn
		if _, err := w.NC.QueueSubscribe(subject, w.Queue, tracedHandler(w.App, subject, fn)); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

// tracedHandler wraps a raw NATS handler in a New Relic transaction named after
// its subject, so every dashboard RPC shows up as its own transaction.
func tracedHandler(app *newrelic.Application, subject string, fn func(context.Context, *nats.Msg)) nats.MsgHandler {
	return func(msg *nats.Msg) {
		txn := app.StartTransaction("rpc " + subject)
		defer txn.End()
		fn(newrelic.NewContext(context.Background(), txn), msg)
	}
}

func respondErr(msg *nats.Msg, text string) { bus.Respond(msg, map[string]any{"error": text}) }
func respondOK(msg *nats.Msg)               { bus.Respond(msg, map[string]any{"ok": true}) }

// decodeRequest unmarshals the message body into T, responding "bad request"
// and returning ok=false on a malformed payload.
func decodeRequest[T any](msg *nats.Msg) (req T, ok bool) {
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondErr(msg, "bad request")
		return req, false
	}
	return req, true
}

// parseWireID parses a decimal id, responding "<field> must be numeric" and
// returning ok=false when it is not. field is the wire field name for the
// error text.
func parseWireID(msg *nats.Msg, raw, field string) (uint64, bool) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		respondErr(msg, field+" must be numeric")
		return 0, false
	}
	return id, true
}

// timeout derives the per-handler repo deadline.
func timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, dashboardTimeout)
}

// publishInvalidate pushes a cache invalidation and logs (never fails the RPC)
// on error. op labels the log line.
func (d *dashboardRPC) publishInvalidate(scope, id, op string) {
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, scope, id); err != nil {
		d.log.Warn(op+" invalidation publish failed", zap.Error(err))
	}
}

// writeThenInvalidate is the shared write path for the "set" verbs: run the
// repo write under the handler deadline, drop the broadcaster's cached view on
// success, and respond ok. op labels both the error log and the invalidation
// warning; scope is the invalidation scope; broadcasterID is the raw wire id.
func (d *dashboardRPC) writeThenInvalidate(ctx context.Context, msg *nats.Msg, scope, broadcasterID, op string, write func(context.Context) error) {
	ctx, cancel := timeout(ctx)
	defer cancel()

	if err := write(ctx); err != nil {
		d.log.Error(op, zap.Error(err))
		respondErr(msg, err.Error())
		return
	}
	d.publishInvalidate(scope, broadcasterID, op)
	respondOK(msg)
}

func (d *dashboardRPC) handleUpsertUser(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.UpsertUserRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	// Ensure email is generated uniquely since we don't fetch it from Twitch by default
	email := fmt.Sprintf("%d@twitch.tv", id)

	if err := d.repo.Register(ctx, id, req.Username, email); err != nil {
		d.log.Error("upsert_user register", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}

	// Capture the real contact email when the callback forwarded one.
	// Best-effort: a storage failure must never bounce a login, and the
	// address itself never reaches the log line.
	if req.Email != "" {
		if err := d.repo.SetContactEmail(ctx, id, req.Email); err != nil {
			d.log.Warn("upsert_user contact email store failed",
				zap.Uint64("user_id", id), zap.Error(err))
		}
	}

	// Push-drop cached account state on every console replica: a recreated
	// account must not keep serving another pod's deleted-era view for the
	// rest of that pod's SWR window.
	d.publishInvalidate("status", req.UserID, "upsert_user")
	respondOK(msg)
}

func (d *dashboardRPC) handleGrantSave(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.GrantSaveRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	d.writeThenInvalidate(ctx, msg, "grant", req.BroadcasterUserID, "grant_save",
		func(ctx context.Context) error {
			return d.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch, []byte(req.AccessToken), []byte(req.RefreshToken))
		})
}

func (d *dashboardRPC) handleGrantHas(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.GrantHasRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	accessToken, _, err := d.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	hasGrant := err == nil && len(accessToken) > 0
	bus.Respond(msg, map[string]any{"has_grant": hasGrant})
}

// handleActiveSet flips the receive toggle. The repository publishes the
// change event, so the projector and ingress converge without extra work;
// the explicit invalidation below covers the dashboard's own grant cache.
func (d *dashboardRPC) handleActiveSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.ActiveSetRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	d.writeThenInvalidate(ctx, msg, "status", req.BroadcasterUserID, "active_set",
		func(ctx context.Context) error { return d.repo.SetActive(ctx, id, req.Active) })
}

func (d *dashboardRPC) handleActiveGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{"active": view.IsActive}
	})
}

// handleStatusGet returns the broadcaster's billing tier (free/paid/vip) so the
// dashboard can show the account status to the user themselves.
func (d *dashboardRPC) handleStatusGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{"status": view.Status, "onboarded": view.Onboarded}
	})
}

// handleStateGet returns both the receive toggle and billing tier in one reply.
// active_get and status_get each load the same user view, so the dashboard's
// page render coalesces them here to spend one round trip and one repo.Get
// instead of two.
func (d *dashboardRPC) handleStateGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{
			"active":                      view.IsActive,
			"status":                      view.Status,
			"onboarded":                   view.Onboarded,
			"locale":                      view.Locale,
			"expires_at":                  view.SubscriptionExpiresAt,
			"source":                      view.SubscriptionSource,
			"subscription_ref":            view.SubscriptionRef,
			"subscription_cancel_pending": view.SubscriptionCancelPending,
		}
	})
}

// readView is the shared read path for active_get / status_get / state_get:
// decode the broadcaster id, load the user view once, then project the reply
// with render.
func (d *dashboardRPC) readView(ctx context.Context, msg *nats.Msg, render func(repository.UserView) map[string]any) {
	req, ok := decodeRequest[usersrpc.GrantHasRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondErr(msg, err.Error())
		return
	}
	bus.Respond(msg, render(view))
}

// handleOnboardedSet saves the user's completion of the onboarding flow.
func (d *dashboardRPC) handleOnboardedSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.OnboardedSetRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	d.writeThenInvalidate(ctx, msg, "status", req.BroadcasterUserID, "onboarded_set",
		func(ctx context.Context) error { return d.repo.SetOnboarded(ctx, id, req.Onboarded) })
}

// supportedLocales is the console's UI language set. Kept here so the service
// rejects a bogus value rather than storing it; it must stay in step with the
// console's LOCALES list (console/shared/lib/i18n).
var supportedLocales = map[string]bool{"en": true, "fr": true}

// handleLocaleSet persists the user's console language preference. The value is
// validated against the supported set so a stray write can't poison the column;
// the console mirrors the same choice into a cookie for fast SSR.
func (d *dashboardRPC) handleLocaleSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.LocaleSetRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}
	if !supportedLocales[req.Locale] {
		respondErr(msg, "unsupported locale")
		return
	}

	// Invalidate the "locale" scope so the console's cached locale view drops on
	// every replica: a change is then visible on the next login/render instead
	// of riding out the SWR window.
	d.writeThenInvalidate(ctx, msg, "locale", req.BroadcasterUserID, "locale_set",
		func(ctx context.Context) error { return d.repo.SetLocale(ctx, id, req.Locale) })
}

// handleDeleteSelf removes the user and every delegation they own. Delegations
// are cleared first so no dangling links survive the deleted user row.
func (d *dashboardRPC) handleDeleteSelf(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeleteSelfRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	if err := d.repo.DeleteDelegationsByOwner(ctx, id); err != nil {
		d.log.Error("delete_self delegations", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}
	if err := d.repo.Delete(ctx, id); err != nil {
		d.log.Error("delete_self user", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}

	// "user" is not a routed scope on the console side, so it falls through to
	// the '*' entry: a coarse per-user flush of every cached prefix. That is
	// exactly right for deletion — no replica may keep any view of this user,
	// and without this ping other pods would serve stale state for the rest of
	// their SWR windows (the deleting pod only drops its own L1).
	d.publishInvalidate("user", req.UserID, "delete_self")
	respondOK(msg)
}
