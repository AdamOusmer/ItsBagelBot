// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// prefField must never include status (money) or banned (moderation).
type prefField uint8

const (
	prefLocale prefField = iota
	prefCursor
	prefCreatorCode
)

type prefKey struct {
	userID uint64
	field  prefField
}

type prefWrite struct {
	userID uint64
	field  prefField
	flag   bool
	str    string
	code   *string
}

func (r *Users) newPreferenceGroup() *batch.Group[prefWrite] {
	return batch.NewGroup(batch.GroupConfig[prefWrite]{
		Window: 3 * time.Millisecond, MaxRows: 256, MaxBytes: 256 * 1024,
		Size: preferenceBytes, Key: preferenceKey, Attributable: unpersistablePrefErr,
		AfterCommit: r.afterPreferences, Timeout: time.Second, QueueTimeout: 250 * time.Millisecond,
		AfterCommitTimeout: 250 * time.Millisecond, Log: r.log,
	}, r.flushPrefs)
}

func preferenceKey(w prefWrite) any { return prefKey{userID: w.userID, field: w.field} }

func preferenceBytes(w prefWrite) int {
	size := 128 + 6*len(w.str)
	if w.code != nil {
		size += 6 * len(*w.code)
	}
	return size
}

func (r *Users) queuePref(ctx context.Context, id uint64, field prefField, w prefWrite) error {
	w.userID = id
	w.field = field
	return r.preferences.Submit(context.WithoutCancel(ctx), w)
}

func (r *Users) SetCustomCursor(ctx context.Context, id uint64, on bool) error {
	return r.queuePref(ctx, id, prefCursor, prefWrite{flag: on})
}

func (r *Users) SetLocale(ctx context.Context, id uint64, locale string) error {
	return r.queuePref(ctx, id, prefLocale, prefWrite{str: locale})
}

func applyPref(u *ent.UserUpdateOne, w prefWrite) {
	switch w.field {
	case prefLocale:
		u.SetLocale(w.str)
	case prefCursor:
		u.SetCustomCursor(w.flag)
	case prefCreatorCode:
		applyCreatorCode(u, w.code)
	}
}

func applyCreatorCode(u *ent.UserUpdateOne, code *string) {
	if code == nil {
		u.ClearCreatorCode()
		return
	}
	u.SetCreatorCode(*code)
}

func (r *Users) flushPrefs(ctx context.Context, items []prefWrite) error {
	ctx, txn, _ := r.beginFlush(ctx, "flush user preferences")
	defer endFlush(txn)
	err := r.persistPrefs(ctx, groupByUser(items))
	txnNotice(txn, err)
	return err
}

func (r *Users) afterPreferences(ctx context.Context, items []prefWrite) {
	for id := range groupByUser(items) {
		r.announcePref(ctx, r.log, id)
	}
}

func (r *Users) beginFlush(ctx context.Context, name string) (context.Context, *newrelic.Transaction, *zap.Logger) {
	if r.app == nil {
		return ctx, nil, r.log
	}
	txn := r.app.StartTransaction(name)
	ctx = newrelic.NewContext(ctx, txn)
	return ctx, txn, monitor.TxnLogger(ctx, r.log)
}

func endFlush(txn *newrelic.Transaction) {
	if txn != nil {
		txn.End()
	}
}

func groupByUser(items []prefWrite) map[uint64][]prefWrite {
	perUser := make(map[uint64][]prefWrite, len(items))
	for _, w := range items {
		perUser[w.userID] = append(perUser[w.userID], w)
	}
	return perUser
}

func writeUserPrefs(ctx context.Context, tx *ent.Tx, id uint64, writes []prefWrite) error {
	update := tx.User.UpdateOneID(id)
	for _, w := range writes {
		applyPref(update, w)
	}
	return update.Exec(ctx)
}

func unpersistablePrefErr(err error) bool {
	return ent.IsValidationError(err) || ent.IsConstraintError(err) || ent.IsNotFound(err)
}

func txnNotice(txn *newrelic.Transaction, err error) {
	if txn != nil {
		txn.NoticeError(err)
	}
}

func (r *Users) announcePref(ctx context.Context, log *zap.Logger, id uint64) {
	if err := r.publishChanged(ctx, id); err != nil {
		log.Error("failed to announce preference change", zap.Uint64("user_id", id), zap.Error(err))
	}
}
