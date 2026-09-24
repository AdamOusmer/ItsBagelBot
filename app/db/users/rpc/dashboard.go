// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
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
		"upsert_user":       d.handleUpsertUser,
		"grant_save":        d.handleGrantSave,
		"grant_has":         d.handleGrantHas,
		"active_set":        d.handleActiveSet,
		"active_get":        d.handleActiveGet,
		"status_get":        d.handleStatusGet,
		"state_get":         d.handleStateGet,
		"login_resolve":     d.handleLoginResolve,
		"onboarded_set":     d.handleOnboardedSet,
		"locale_set":        d.handleLocaleSet,
		"cursor_set":        d.handleCursorSet,
		"commands_page_set": d.handleCommandsPageSet,
		"delete_self":       d.handleDeleteSelf,
	}
	for verb, fn := range verbs {
		subject := prefix + "." + verb
		fn := fn
		if err := bus.QueueSubscribeRPC(w.NC, subject, w.Queue, tracedHandler(w.App, subject, fn)); err != nil {
			return fmt.Errorf("subscribe %s: %w", subject, err)
		}
	}
	return nil
}

func tracedHandler(app *newrelic.Application, subject string, fn func(context.Context, *nats.Msg)) nats.MsgHandler {
	return func(msg *nats.Msg) {
		txn := app.StartTransaction("rpc " + subject)
		defer txn.End()
		fn(newrelic.NewContext(context.Background(), txn), msg)
	}
}

func respondErr(msg *nats.Msg, text string) { bus.Respond(msg, map[string]any{"error": text}) }
func respondOK(msg *nats.Msg)               { bus.Respond(msg, map[string]any{"ok": true}) }

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

func timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, dashboardTimeout)
}

func (d *dashboardRPC) publishInvalidate(scope, id, op string) {
	if err := invalidate.Publish(d.nc, d.invalidationPrefix, scope, id); err != nil {
		d.log.Warn(op+" invalidation publish failed", zap.Error(err))
	}
}

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

	ctx, cancel := timeout(ctx)
	defer cancel()

	email := fmt.Sprintf("%d@twitch.tv", id)

	if err := d.repo.Register(ctx, id, req.Username, req.DisplayName, email); err != nil {
		log.Error("upsert_user register", zap.Error(err))
		respondErr(msg, err.Error())
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

	ctx, cancel := timeout(ctx)
	defer cancel()

	accessToken, _, _, err := d.repo.Token(ctx, id, tokens.TypeUserToken, tokens.PlatformTwitch)
	hasGrant := err == nil && len(accessToken) > 0
	bus.Respond(msg, map[string]any{"has_grant": hasGrant})
}

func (d *dashboardRPC) handleActiveSet(ctx context.Context, msg *nats.Msg) {
	setBoolPref(d, ctx, msg, "status", "active_set",
		func(r usersrpc.ActiveSetRequest) string { return r.BroadcasterUserID },
		func(ctx context.Context, id uint64, r usersrpc.ActiveSetRequest) error {
			return d.repo.SetActive(ctx, id, r.Active)
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
			"username":                    view.Username,
			"display_name":                view.DisplayName,
			"status":                      view.Status,
			"onboarded":                   view.Onboarded,
			"locale":                      view.Locale,
			"custom_cursor":               view.CustomCursor,
			"commands_page_hidden":        view.CommandsPageHidden,
			"creator_code":                view.CreatorCode,
			"expires_at":                  view.SubscriptionExpiresAt,
			"source":                      view.SubscriptionSource,
			"subscription_ref":            view.SubscriptionRef,
			"subscription_cancel_pending": view.SubscriptionCancelPending,
		}
	})
}

func (d *dashboardRPC) handleLoginResolve(ctx context.Context, msg *nats.Msg) {
	req, ok := decodeRequest[usersrpc.LoginResolveRequest](msg)
	if !ok {
		return
	}

	ctx, cancel := timeout(ctx)
	defer cancel()

	id, err := d.repo.IDByUsername(ctx, req.Login)
	if err != nil {
		respondErr(msg, "not found")
		return
	}
	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondErr(msg, err.Error())
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

	ctx, cancel := timeout(ctx)
	defer cancel()

	view, err := d.repo.Get(ctx, id)
	if err != nil {
		respondErr(msg, err.Error())
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
		func(ctx context.Context) error { return d.repo.SetLocale(ctx, id, req.Locale) })
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

	log := monitor.TxnLogger(ctx, d.log)
	delegateIDs, err := d.repo.DeleteDelegationsByOwner(ctx, id)
	if err != nil {
		log.Error("delete_self delegations", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}
	for _, delID := range delegateIDs {
		d.publishInvalidate("delegation", fmt.Sprint(delID), "delete_self")
	}
	if err := d.repo.Delete(ctx, id); err != nil {
		log.Error("delete_self user", zap.Error(err))
		respondErr(msg, err.Error())
		return
	}

	d.publishInvalidate("user", req.UserID, "delete_self")
	respondOK(msg)
}
