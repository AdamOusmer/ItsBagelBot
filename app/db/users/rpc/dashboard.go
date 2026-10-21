// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/invalidate"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/monitor"
)

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
		"upsert_user":          d.handleUpsertUser,
		"grant_save":           d.handleGrantSave,
		"grant_has":            d.handleGrantHas,
		"active_set":           d.handleActiveSet,
		"active_get":           d.handleActiveGet,
		"status_get":           d.handleStatusGet,
		"state_get":            d.handleStateGet,
		"lifecycle_retry":      d.handleLifecycleRetry,
		"login_resolve":        d.handleLoginResolve,
		"onboarded_set":        d.handleOnboardedSet,
		"locale_set":           d.handleLocaleSet,
		"cursor_set":           d.handleCursorSet,
		"commands_page_set":    d.handleCommandsPageSet,
		"leaderboard_page_set": d.handleLeaderboardPageSet,
		"delete_self":          d.handleDeleteSelf,
	}
	return serveMsgVerbs(w.Within(dashboardTimeout), prefix, verbs)
}

func serveMsgVerbs(w bus.RPCWiring, prefix string, verbs map[string]func(context.Context, *nats.Msg)) error {
	for verb, fn := range verbs {
		if err := bus.ServeMsg(w, prefix+"."+verb, fn); err != nil {
			return err
		}
	}
	return nil
}

func respondErr(msg *nats.Msg, text string) {
	bus.Respond(msg, domainrpc.Refused(domainrpc.CodeInvalid, text))
}

func respondFail(msg *nats.Msg, err error, rules ...domainrpc.Rule) {
	rules = append(rules, domainrpc.When(ent.IsNotFound, domainrpc.CodeNotFound))
	bus.Respond(msg, domainrpc.Fail(err, rules...))
}

func respondOK(msg *nats.Msg) { bus.Respond(msg, map[string]any{"ok": true}) }

func decodeRequest[T any](msg *nats.Msg) (req T, ok bool) {
	if err := codec.Unmarshal(msg.Data, &req); err != nil {
		respondErr(msg, "bad request")
		return req, false
	}
	return req, true
}

func parseWireID(msg *nats.Msg, raw, field string) (uint64, bool) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		respondErr(msg, field+" must be numeric")
		return 0, false
	}
	return id, true
}

func (d *dashboardRPC) publishInvalidate(scope, id, op string) {
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, scope, id); err != nil {
		d.log.Warn(op+" invalidation publish failed", zap.Error(err))
	}
}

func (d *dashboardRPC) writeThenInvalidate(ctx context.Context, msg *nats.Msg, scope, broadcasterID, op string, write func(context.Context) error) {
	if err := write(ctx); err != nil {
		d.log.Error(op, zap.Error(err))
		respondFail(msg, err)
		return
	}
	d.publishInvalidate(scope, broadcasterID, op)
	respondOK(msg)
}

func setBoolPref[T any](d *dashboardRPC, ctx context.Context, msg *nats.Msg, scope, op string,
	broadcaster func(T) string, write func(context.Context, uint64, T) error) {
	req, ok := decodeRequest[T](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, broadcaster(req), "broadcaster_user_id")
	if !ok {
		return
	}
	d.writeThenInvalidate(ctx, msg, scope, broadcaster(req), op,
		func(ctx context.Context) error { return write(ctx, id, req) })
}

func (d *dashboardRPC) handleUpsertUser(ctx context.Context, msg *nats.Msg) {
	log := monitor.TxnLogger(ctx, d.log)
	req, ok := decodeRequest[usersrpc.UpsertUserRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}

	email := fmt.Sprintf("%d@twitch.tv", id)

	if err := d.repo.Register(ctx, id, req.Username, req.DisplayName, email); err != nil {
		log.Error("upsert_user register", zap.Error(err))
		respondFail(msg, err)
		return
	}

	// Best effort: must never bounce a login or log the address.
	if req.Email != "" {
		if err := d.repo.SetContactEmail(ctx, id, req.Email); err != nil {
			log.Warn("upsert_user contact email store failed",
				zap.Uint64("user_id", id), zap.Error(err))
		}
	}

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
			return d.repo.UpsertToken(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch, []byte(req.AccessToken), []byte(req.RefreshToken), nil)
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

	accessToken, _, _, err := d.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	hasGrant := err == nil && len(accessToken) > 0
	bus.Respond(msg, map[string]any{"has_grant": hasGrant})
}

func (d *dashboardRPC) handleActiveSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "status", "active_set",
		func(r usersrpc.ActiveSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.ActiveSetRequest) error {
			return d.repo.SetActiveNow(ctx, id, r.Active)
		})
}

func (d *dashboardRPC) handleActiveGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{"active": view.IsActive}
	})
}

func (d *dashboardRPC) handleStatusGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{"status": view.Status, "onboarded": view.Onboarded}
	})
}

func (d *dashboardRPC) handleStateGet(ctx context.Context, msg *nats.Msg) {
	d.readView(ctx, msg, func(view repository.UserView) map[string]any {
		return map[string]any{
			"active":                      view.IsActive,
			"lifecycle":                   view.Lifecycle,
			"state_revision":              view.StateRevision,
			"account_created_at":          view.AccountCreatedAt,
			"username":                    view.Username,
			"display_name":                view.DisplayName,
			"status":                      view.Status,
			"onboarded":                   view.Onboarded,
			"locale":                      view.Locale,
			"custom_cursor":               view.CustomCursor,
			"commands_page_hidden":        view.CommandsPageHidden,
			"leaderboard_page_hidden":     view.LeaderboardPageHidden,
			"creator_code":                view.CreatorCode,
			"expires_at":                  view.SubscriptionExpiresAt,
			"source":                      view.SubscriptionSource,
			"subscription_ref":            view.SubscriptionRef,
			"subscription_cancel_pending": view.SubscriptionCancelPending,
			"subscription_payment_failed": view.SubscriptionPaymentFailed,
		}
	})
}

func (d *dashboardRPC) handleLoginResolve(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.LoginResolveRequest](msg)
	if !ok {
		return
	}

	id, err := d.repo.IDByUsername(ctx, req.Login)
	if err != nil {
		respondFail(msg, err, domainrpc.Is(validate.ErrUsernameInvalid, domainrpc.CodeInvalid))
		return
	}
	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondFail(msg, err)
		return
	}
	bus.Respond(msg, map[string]any{
		"user_id":      strconv.FormatUint(view.ID, 10),
		"username":     view.Username,
		"display_name": view.DisplayName,
	})
}

func (d *dashboardRPC) readView(ctx context.Context, msg *nats.Msg, render func(repository.UserView) map[string]any) {
	req, ok := decodeRequest[usersrpc.GrantHasRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondFail(msg, err)
		return
	}
	bus.Respond(msg, render(view))
}

func (d *dashboardRPC) handleOnboardedSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "status", "onboarded_set",
		func(r usersrpc.OnboardedSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.OnboardedSetRequest) error {
			return d.repo.SetOnboarded(ctx, id, r.Onboarded)
		})
}

func (d *dashboardRPC) handleLocaleSet(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.LocaleSetRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}
	if !i18n.Supported(req.Locale) {
		respondErr(msg, "unsupported locale")
		return
	}

	d.writeThenInvalidate(ctx, msg, "locale", req.BroadcasterUserID, "locale_set",
		func(ctx context.Context) error { return d.repo.SetLocaleNow(ctx, id, req.Locale) })
}

func (d *dashboardRPC) handleCursorSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "cursor", "cursor_set",
		func(r usersrpc.CursorSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.CursorSetRequest) error {
			return d.repo.SetCustomCursor(ctx, id, r.CustomCursor)
		})
}

func (d *dashboardRPC) handleCommandsPageSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "commands_page", "commands_page_set",
		func(r usersrpc.CommandsPageSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.CommandsPageSetRequest) error {
			return d.repo.SetCommandsPageHidden(ctx, id, r.Hidden)
		})
}

func (d *dashboardRPC) handleLeaderboardPageSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "leaderboard_page", "leaderboard_page_set",
		func(r usersrpc.LeaderboardPageSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.LeaderboardPageSetRequest) error {
			return d.repo.SetLeaderboardPageHidden(ctx, id, r.Hidden)
		})
}

func (d *dashboardRPC) handleDeleteSelf(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.DeleteSelfRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.UserID, "user_id")
	if !ok {
		return
	}

	log := monitor.TxnLogger(ctx, d.log)
	delegateIDs, err := d.repo.DeleteDelegationsByOwner(ctx, id)
	if err != nil {
		log.Error("delete_self delegations", zap.Error(err))
		respondFail(msg, err)
		return
	}
	for _, delID := range delegateIDs {
		d.publishInvalidate("delegation", fmt.Sprint(delID), "delete_self")
	}
	if err := d.repo.Delete(ctx, id); err != nil {
		log.Error("delete_self user", zap.Error(err))
		respondFail(msg, err)
		return
	}

	d.publishInvalidate("user", req.UserID, "delete_self")
	respondOK(msg)
}

func (d *dashboardRPC) handleLifecycleRetry(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.StateGetRequest](msg)
	if !ok {
		return
	}
	id, ok := parseWireID(msg, req.BroadcasterUserID, "broadcaster_user_id")
	if !ok {
		return
	}
	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondFail(msg, err)
		return
	}
	rep, err := d.repo.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: id, AccountCreatedAt: view.AccountCreatedAt, Action: "retry"})
	if err != nil {
		respondFail(msg, err)
		return
	}
	bus.Respond(msg, rep)
}
