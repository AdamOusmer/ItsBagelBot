// Package rpc exposes the loyalty service's NATS request/reply surface
// (bagel.rpc.loyalty.*). Sesame is the primary caller: balance/counter reads
// on a cache miss, and the mod-facing counter management verbs behind
// !counter. The future dashboard pages ride the same verbs.
package rpc

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"

	"ItsBagelBot/app/loyalty/ent"
	"ItsBagelBot/app/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/bus"
)

const handleTimeout = 2 * time.Second

type loyaltyRPC struct {
	repo *repository.Loyalty
	log  *zap.Logger
}

// Subscribe registers the loyalty verbs under prefix:
//
//	<prefix>.balance.get    {user_id, viewer_id}            -> {balance}
//	<prefix>.balance.set    {user_id, viewer_login, value}  -> {balance, found}
//	<prefix>.balance.add    {user_id, viewer_login, value}  -> {balance, found}
//	<prefix>.top.get        {user_id, limit}                -> {top}
//	<prefix>.counter.get    {user_id, name[, viewer_id, command]} -> {counter, found}
//	<prefix>.counter.create {user_id, name, scope}          -> {counter}
//	<prefix>.counter.set    {user_id, name, value[, viewer_id, command]} -> {found}
//	<prefix>.counter.rename {user_id, name, new_name}       -> {found}
//	<prefix>.counter.delete {user_id, name}                 -> {}
//	<prefix>.counter.entry.delete {user_id, name, viewer_id|command} -> {found}
//	<prefix>.counter.list   {user_id}                       -> {counters}
//	<prefix>.counter.entries {user_id, name, limit}         -> {entries, found}
//
// Counter verbs accept user_id "0": the reserved bot namespace holding the
// admin-only bot-scope counters. Balance verbs always require a broadcaster.
func Subscribe(nc *nats.Conn, repo *repository.Loyalty, prefix, queueGroup string, app *newrelic.Application, log *zap.Logger) error {
	l := &loyaltyRPC{repo: repo, log: log}

	verbs := []struct {
		verb    string
		handler func(context.Context, loyaltyrpc.Request) loyaltyrpc.Reply
	}{
		{"balance.get", l.handleBalanceGet},
		{"balance.set", l.handleBalanceSet},
		{"balance.add", l.handleBalanceAdd},
		{"top.get", l.handleTopGet},
		{"counter.get", l.handleCounterGet},
		{"counter.create", l.handleCounterCreate},
		{"counter.set", l.handleCounterSet},
		{"counter.rename", l.handleCounterRename},
		{"counter.delete", l.handleCounterDelete},
		{"counter.entry.delete", l.handleCounterEntryDelete},
		{"counter.list", l.handleCounterList},
		{"counter.entries", l.handleCounterEntries},
	}

	for _, v := range verbs {
		subject := prefix + "." + v.verb
		if err := bus.QueueSubscribeJSON[loyaltyrpc.Request, loyaltyrpc.Reply](nc, subject, queueGroup, handleTimeout, app, log, v.handler); err != nil {
			return err
		}
	}
	return nil
}

// parseIDs pulls the broadcaster id (required) and viewer id (optional) off a
// request. ok=false carries the error reply. allowBotNS admits user_id "0" —
// the reserved bot namespace the counter verbs use for admin-only bot-scope
// counters; balance verbs always require a real broadcaster.
func parseIDs(req loyaltyrpc.Request, allowBotNS bool) (userID, viewerID uint64, ok bool, reply loyaltyrpc.Reply) {
	uid, err := strconv.ParseUint(req.UserID, 10, 64)
	if err != nil {
		return 0, 0, false, loyaltyrpc.Reply{Error: "invalid user_id"}
	}
	if uid == 0 && !allowBotNS {
		return 0, 0, false, loyaltyrpc.Reply{Error: "invalid user_id"}
	}
	if req.ViewerID != "" {
		vid, err := strconv.ParseUint(req.ViewerID, 10, 64)
		if err != nil {
			return 0, 0, false, loyaltyrpc.Reply{Error: "invalid viewer_id"}
		}
		viewerID = vid
	}
	return uid, viewerID, true, loyaltyrpc.Reply{}
}

// fail maps a repository error: trust-boundary rejections echo their message,
// anything else is logged and masked.
func (l *loyaltyRPC) fail(op string, err error) loyaltyrpc.Reply {
	if errors.Is(err, repository.ErrInvalidInput) {
		return loyaltyrpc.Reply{Error: err.Error()}
	}
	l.log.Warn(op+" failed", zap.Error(err))
	return loyaltyrpc.Reply{Error: "loyalty request failed"}
}

func balanceView(row *ent.Balance) *loyaltyrpc.Balance {
	return &loyaltyrpc.Balance{
		ViewerID:     strconv.FormatUint(row.ViewerID, 10),
		ViewerLogin:  row.ViewerLogin,
		ViewerName:   row.ViewerName,
		Points:       row.Points,
		WatchSeconds: row.WatchSeconds,
	}
}

func (l *loyaltyRPC) handleBalanceGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	if viewerID == 0 {
		return loyaltyrpc.Reply{Error: "invalid viewer_id"}
	}
	row, found, err := l.repo.BalanceGet(ctx, userID, viewerID)
	if err != nil {
		return l.fail("loyalty balance.get", err)
	}
	if !found {
		// A viewer with no row simply has nothing yet; zero is the answer.
		return loyaltyrpc.Reply{Balance: &loyaltyrpc.Balance{ViewerID: req.ViewerID}, Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true}
}

// handleBalanceSet / handleBalanceAdd back the mod grants ("!points set/add
// @user") and the future dashboard equivalents. The target is addressed by
// login; found=false means no accrual has ever seen them in this channel.
func (l *loyaltyRPC) handleBalanceSet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.adjustBalance(ctx, req, true)
}

func (l *loyaltyRPC) handleBalanceAdd(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	return l.adjustBalance(ctx, req, false)
}

func (l *loyaltyRPC) adjustBalance(ctx context.Context, req loyaltyrpc.Request, absolute bool) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	row, found, err := l.repo.BalanceAdjust(ctx, userID, req.ViewerLogin, req.Value, absolute)
	if err != nil {
		return l.fail("loyalty balance adjust", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{Balance: balanceView(row), Found: true}
}

func (l *loyaltyRPC) handleCounterEntries(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	rows, logins, err := l.repo.CounterEntries(ctx, userID, req.Name, req.Limit)
	if err != nil {
		return l.fail("loyalty counter.entries", err)
	}
	entries := make([]loyaltyrpc.CounterEntry, 0, len(rows))
	for _, e := range rows {
		login := e.ViewerLogin
		if login == "" {
			login = logins[e.ViewerID] // legacy rows written before identity was stored
		}
		entries = append(entries, loyaltyrpc.CounterEntry{
			ViewerID:    strconv.FormatUint(e.ViewerID, 10),
			ViewerLogin: login,
			ViewerName:  e.ViewerName,
			Command:     e.Command,
			Value:       e.Value,
		})
	}
	return loyaltyrpc.Reply{Entries: entries, Found: true}
}

func (l *loyaltyRPC) handleTopGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, false)
	if !ok {
		return reply
	}
	rows, err := l.repo.Top(ctx, userID, req.Limit)
	if err != nil {
		return l.fail("loyalty top.get", err)
	}
	top := make([]loyaltyrpc.Balance, 0, len(rows))
	for _, row := range rows {
		top = append(top, *balanceView(row))
	}
	return loyaltyrpc.Reply{Top: top, Found: true}
}

func (l *loyaltyRPC) handleCounterGet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	row, value, found, err := l.repo.CounterGet(ctx, userID, req.Name, viewerID, req.Command)
	if err != nil {
		return l.fail("loyalty counter.get", err)
	}
	if !found {
		return loyaltyrpc.Reply{Found: false}
	}
	return loyaltyrpc.Reply{
		Counter: &loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: value},
		Found:   true,
	}
}

func (l *loyaltyRPC) handleCounterCreate(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	row, err := l.repo.CounterCreate(ctx, userID, req.Name, req.Scope)
	if err != nil {
		return l.fail("loyalty counter.create", err)
	}
	return loyaltyrpc.Reply{
		Counter: &loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: row.Value},
		Found:   true,
	}
}

// foundReply maps the (found, err) pair the counter write verbs share: a
// repository failure through fail, everything else into a bare Found reply.
func (l *loyaltyRPC) foundReply(op string, found bool, err error) loyaltyrpc.Reply {
	if err != nil {
		return l.fail(op, err)
	}
	return loyaltyrpc.Reply{Found: found}
}

func (l *loyaltyRPC) handleCounterSet(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	target := repository.SetTarget{ViewerID: viewerID, Command: req.Command, ViewerLogin: req.ViewerLogin}
	found, err := l.repo.CounterSet(ctx, userID, req.Name, target, req.Value)
	return l.foundReply("loyalty counter.set", found, err)
}

// handleCounterRename moves a counter (and its entry buckets) to a new name;
// found=false means no counter carries the old name.
func (l *loyaltyRPC) handleCounterRename(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	found, err := l.repo.CounterRename(ctx, userID, req.Name, req.NewName)
	return l.foundReply("loyalty counter.rename", found, err)
}

func (l *loyaltyRPC) handleCounterDelete(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	err := l.repo.CounterDelete(ctx, userID, req.Name)
	return l.foundReply("loyalty counter.delete", true, err)
}

// handleCounterEntryDelete removes one stored bucket of an entry-scoped
// counter, addressed by viewer_id and/or command; found=false means no such
// counter, a non-entry scope, or an untargeted address (which is refused so it
// can never become a whole-counter reset).
func (l *loyaltyRPC) handleCounterEntryDelete(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, viewerID, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	target := repository.SetTarget{ViewerID: viewerID, Command: req.Command}
	found, err := l.repo.CounterEntryDelete(ctx, userID, req.Name, target)
	return l.foundReply("loyalty counter.entry.delete", found, err)
}

func (l *loyaltyRPC) handleCounterList(ctx context.Context, req loyaltyrpc.Request) loyaltyrpc.Reply {
	userID, _, ok, reply := parseIDs(req, true)
	if !ok {
		return reply
	}
	rows, err := l.repo.CountersList(ctx, userID)
	if err != nil {
		return l.fail("loyalty counter.list", err)
	}
	counters := make([]loyaltyrpc.Counter, 0, len(rows))
	for _, row := range rows {
		counters = append(counters, loyaltyrpc.Counter{Name: row.Name, Scope: row.Scope, Value: row.Value})
	}
	return loyaltyrpc.Reply{Counters: counters, Found: true}
}
