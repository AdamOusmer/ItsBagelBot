// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	// prefsFlushInterval and prefsFlushMaxSize are the same write-behind
	// window ADR-0008 prescribes for modules and commands: a preference
	// clicked five times in two seconds costs one row write, whichever bound
	// trips first drains the window.
	prefsFlushInterval = 2 * time.Second
	prefsFlushMaxSize  = 256
)

// prefField enumerates the user columns served through the write-behind
// batcher. Money (status tier) and moderation (banned) deliberately stay off
// this list; see SetStatus and SetBanned.
type prefField uint8

const (
	prefActive prefField = iota
	prefLocale
	prefCursor
	prefOnboarded
	prefCreatorCode
)

// prefKey coalesces on (user, field): repeated clicks on ONE preference
// collapse into the latest value, while different preferences of the same
// user stay separate pending entries that merge into one UPDATE at flush.
type prefKey struct {
	userID uint64
	field  prefField
}

// prefWrite is one queued preference write. It repeats its key inside the
// value because the flush callback receives values only.
type prefWrite struct {
	userID uint64
	field  prefField

	flag bool    // active / cursor / onboarded
	str  string  // locale
	code *string // creator code; nil clears the nullable column
}

func (r *Users) queuePref(id uint64, field prefField, w prefWrite) {
	w.userID = id
	w.field = field
	r.batcher.Add(prefKey{userID: id, field: field}, w)
}

// applyPref stamps one queued write onto an update builder.
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

// flushPrefs lands one window of coalesced preference writes, then announces
// every changed user once. It runs detached from any request, so it reports
// as its own background transaction. It always returns nil: failures are
// handled here (per-user fallback + requeue), because returning them would
// make the batcher retry the whole window around rows it already dropped.
func (r *Users) flushPrefs(ctx context.Context, items []prefWrite) error {

	var txn *newrelic.Transaction
	if r.app != nil {
		txn = r.app.StartTransaction("flush user preferences")
		defer txn.End()
		ctx = newrelic.NewContext(ctx, txn)
	}
	log := r.log
	if txn != nil {
		log = monitor.TxnLogger(ctx, r.log)
	}

	// Merge per user first: one user touching three preferences in a window
	// gets ONE UPDATE carrying all three columns, and is announced once.
	perUser := make(map[uint64][]prefWrite, len(items))
	for _, w := range items {
		perUser[w.userID] = append(perUser[w.userID], w)
	}

	// The whole window is one transaction, per ADR-0008's "a burst of writes
	// must land as one transaction, not as N round trips".
	err := withTx(ctx, r.client, func(tx *ent.Tx) error {
		for id, writes := range perUser {
			update := tx.User.UpdateOneID(id)
			for _, w := range writes {
				applyPref(update, w)
			}
			if err := update.Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		for id := range perUser {
			r.announcePref(ctx, log, id)
		}
		return nil
	}

	if txn != nil {
		txn.NoticeError(err)
	}
	log.Warn("preference window failed as one transaction, falling back per user",
		zap.Int("users", len(perUser)),
		zap.Error(err),
	)
	r.applyPrefEach(ctx, txn, log, perUser)
	return nil
}

// applyPrefEach retries a failed window one user at a time so a single poison
// row cannot wedge the rest. Rows the database will never accept (validation,
// constraint, or a user deleted mid-window) are dropped with a log; transiently
// failing users are requeued into the batcher for the next window.
func (r *Users) applyPrefEach(ctx context.Context, txn *newrelic.Transaction, log *zap.Logger, perUser map[uint64][]prefWrite) {
	for id, writes := range perUser {
		err := withTx(ctx, r.client, func(tx *ent.Tx) error {
			update := tx.User.UpdateOneID(id)
			for _, w := range writes {
				applyPref(update, w)
			}
			return update.Exec(ctx)
		})
		if err == nil {
			r.announcePref(ctx, log, id)
			continue
		}

		if txn != nil {
			txn.NoticeError(err)
		}
		if ent.IsValidationError(err) || ent.IsConstraintError(err) || ent.IsNotFound(err) {
			log.Error("dropping unpersistable preference change",
				zap.Uint64("user_id", id),
				zap.Int("writes", len(writes)),
				zap.Error(err),
			)
			continue
		}
		log.Warn("requeueing preference changes after transient flush failure",
			zap.Uint64("user_id", id),
			zap.Int("writes", len(writes)),
			zap.Error(err),
		)
		for _, w := range writes {
			r.batcher.Requeue(prefKey{userID: w.userID, field: w.field}, w)
		}
	}
}

// announcePref drops the local cached view and publishes the full new state,
// mirroring what the old synchronous setters did after their commit — but from
// the flush, so events only ever describe committed rows.
func (r *Users) announcePref(ctx context.Context, log *zap.Logger, id uint64) {
	if err := r.publishChanged(ctx, id); err != nil {
		log.Error("failed to announce preference change",
			zap.Uint64("user_id", id),
			zap.Error(err),
		)
	}
}
