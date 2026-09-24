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

const (
	prefsFlushInterval = 2 * time.Second
	prefsFlushMaxSize  = 256
)

// prefField must never include status (money) or banned (moderation).
type prefField uint8

const (
	prefActive prefField = iota
	prefLocale
	prefCursor
	prefOnboarded
	prefCreatorCode
)

type prefKey struct {
	userID uint64
	field  prefField
}

type prefWrite struct {
	userID uint64
	field  prefField

	flag bool
	str  string
	code *string
}

func (r *Users) queuePref(id uint64, field prefField, w prefWrite) {
	w.userID = id
	w.field = field
	r.pendingMu.Lock()
	r.pendingPrefs[prefKey{userID: id, field: field}] = w
	r.pendingMu.Unlock()
	r.batcher.Add(prefKey{userID: id, field: field}, w)
}

func (r *Users) clearPendingPrefs(items []prefWrite) {
	r.pendingMu.Lock()
	defer r.pendingMu.Unlock()
	for _, item := range items {
		key := prefKey{userID: item.userID, field: item.field}
		if current, ok := r.pendingPrefs[key]; ok && samePrefWrite(current, item) {
			delete(r.pendingPrefs, key)
		}
	}
}

func samePrefWrite(a, b prefWrite) bool {
	if !samePrefKey(a, b) {
		return false
	}
	if a.str != b.str {
		return false
	}
	return sameCode(a.code, b.code)
}

func samePrefKey(a, b prefWrite) bool {
	return a.userID == b.userID && a.field == b.field && a.flag == b.flag
}

func sameCode(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func applyPref(u *ent.UserUpdateOne, w prefWrite) {
	switch w.field {
	case prefActive:
		u.SetIsActive(w.flag)
	case prefLocale:
		u.SetLocale(w.str)
	case prefCursor:
		u.SetCustomCursor(w.flag)
	case prefOnboarded:
		u.SetOnboarded(w.flag)
	case prefCreatorCode:
		if w.code == nil {
			u.ClearCreatorCode()
		} else {
			u.SetCreatorCode(*w.code)
		}
	}
}

// Must return nil: returning errors makes the batcher retry rows already dropped.
func (r *Users) flushPrefs(ctx context.Context, items []prefWrite) error {
	ctx, txn, log := r.beginFlush(ctx, "flush user preferences")
	defer endFlush(txn)

	perUser := groupByUser(items)

	err := withTx(ctx, r.client, func(tx *ent.Tx) error {
		return writeAllUserPrefs(ctx, tx, perUser)
	})
	if err == nil {
		r.clearPendingPrefs(items)
		r.announcePrefs(ctx, log, perUser)
		return nil
	}

	txnNotice(txn, err)
	log.Warn("preference window failed as one transaction, falling back per user",
		zap.Int("users", len(perUser)),
		zap.Error(err),
	)
	r.applyPrefEach(ctx, txn, log, perUser)
	return nil
}

const prefAnnounceTimeout = 5 * time.Second

func (r *Users) announcePrefs(ctx context.Context, log *zap.Logger, perUser map[uint64][]prefWrite) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prefAnnounceTimeout)
	defer cancel()

	for id := range perUser {
		r.announcePref(actx, log, id)
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

func writeAllUserPrefs(ctx context.Context, tx *ent.Tx, perUser map[uint64][]prefWrite) error {
	for id, writes := range perUser {
		if err := writeUserPrefs(ctx, tx, id, writes); err != nil {
			return err
		}
	}
	return nil
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

func requeuePrefWrites(b *batch.Batcher[prefKey, prefWrite], writes []prefWrite) {
	for _, w := range writes {
		b.Requeue(prefKey{userID: w.userID, field: w.field}, w)
	}
}

func (r *Users) applyPrefEach(ctx context.Context, txn *newrelic.Transaction, log *zap.Logger, perUser map[uint64][]prefWrite) {
	for id, writes := range perUser {
		err := withTx(ctx, r.client, func(tx *ent.Tx) error {
			return writeUserPrefs(ctx, tx, id, writes)
		})

		switch {
		case err == nil:
			r.clearPendingPrefs(writes)
			r.announcePrefs(ctx, log, map[uint64][]prefWrite{id: writes})
		case unpersistablePrefErr(err):
			txnNotice(txn, err)
			log.Error("dropping unpersistable preference change",
				zap.Uint64("user_id", id),
				zap.Int("writes", len(writes)),
				zap.Error(err),
			)
		default:
			txnNotice(txn, err)
			log.Warn("requeueing preference changes after transient flush failure",
				zap.Uint64("user_id", id),
				zap.Int("writes", len(writes)),
				zap.Error(err),
			)
			requeuePrefWrites(r.batcher, writes)
		}
	}
}

func txnNotice(txn *newrelic.Transaction, err error) {
	if txn != nil {
		txn.NoticeError(err)
	}
}

func (r *Users) announcePref(ctx context.Context, log *zap.Logger, id uint64) {
	if err := r.publishChanged(ctx, id); err != nil {
		log.Error("failed to announce preference change",
			zap.Uint64("user_id", id),
			zap.Error(err),
		)
	}
}
