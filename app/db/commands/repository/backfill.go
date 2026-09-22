// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strings"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/migrations"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

// bumpCounterBackfillName gates BackfillBumpCounterFromTokens to run exactly
// once per database: a row under this name in the migrations table means it
// already ran, so a broadcaster who later clears the bump_counter option
// (deliberately setting it back to "") is never re-populated by a restart.
const bumpCounterBackfillName = "bump_counter_from_counter_token"

// BackfillBumpCounterFromTokens is a one-time migration for the {counter:x}
// deprecation: bump_counter defaults to "", so every command whose response
// still carries the old WRITE spelling ({counter:<name>}, back when that
// token bumped) would otherwise stop incrementing its counter the moment
// this deploy lands, silently, with no error anywhere a broadcaster could
// see. This runs once at boot (see main.go, right after the repository is
// constructed) and derives a bump_counter option from whichever response
// still names one, so the behavior a broadcaster is used to survives the
// migration without them having to notice and re-configure it by hand.
//
// A row is eligible when bump_counter is unset (empty: never touched by
// this migration and never set by hand) and the response contains the
// literal "{counter:" substring (a cheap SQL prefilter; the real parse is
// pkg/tmpl.Lex, run only on the rows that pass it). The first bare counter
// token (no payload prefix; a payload only ever carries "target:...") wins
// — the option is channel-level, it has no per-viewer address to carry, so
// a target-addressed span is not a candidate and is logged rather than
// silently dropped.
//
// Writes go through backfillRow, not the write-behind batcher Upsert uses:
// the marker row must not be recorded until every eligible row has actually
// landed, and a value sitting in the batcher's in-memory window is not yet
// landed — a crash between "queued" and "flushed" would mark the migration
// done with the write never having happened. A direct write is also what
// lets this walk finish and record its marker before main.go moves on to
// serving traffic.
// A failure partway through (a row write error, or the process dying before
// the marker lands) is safe to retry on the next boot without double-work:
// every row this pass already wrote now has a non-empty bump_counter, so the
// BumpCounterEQ("") filter excludes it from the next scan on its own, with
// no separate progress-tracking needed.
func (r *Commands) BackfillBumpCounterFromTokens(ctx context.Context) error {
	applied, err := db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.Migrations.Query().Where(migrations.NameEQ(bumpCounterBackfillName)).Exist(ctx)
	})
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().
			Where(commands.BumpCounterEQ(""), commands.ResponseContains("{counter:")).
			All(ctx)
	})
	if err != nil {
		return err
	}

	for _, row := range rows {
		name, ok := bumpCounterFromResponse(row.Response, row.Name, r.log)
		if !ok {
			continue
		}
		if err := r.backfillRow(ctx, row.UserID, row.Name, name); err != nil {
			r.log.Error("bump_counter backfill: row write failed",
				zap.Uint64("user_id", row.UserID), zap.String("command", row.Name), zap.Error(err))
		}
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.Migrations.Create().SetName(bumpCounterBackfillName).Exec(ctx)
	})
}

// bumpCounterFromResponse lexes a command's stored response for the first
// bare {counter:<name>} span (HasPayload with no ':' inside it — a ':'
// means "target:...", the per-viewer addressed form, which the channel-level
// option cannot carry). Every addressed span it passes over is logged at
// info with the command name, per the decision above, and does not stop the
// scan: an earlier addressed span must not hide a later bare one.
func bumpCounterFromResponse(response, cmdName string, log *zap.Logger) (name string, ok bool) {
	for _, tok := range tmpl.Lex(response) {
		if tok.Kind != tmpl.KindVar || tok.Name != "counter" || !tok.HasPayload {
			continue
		}
		if strings.Contains(tok.Payload, ":") {
			log.Info("bump_counter backfill: skipping a target-addressed counter span; the option is channel-level",
				zap.String("command", cmdName), zap.String("payload", tok.Payload))
			continue
		}
		if !ok {
			name, ok = tmpl.NormalizeName(tok.Payload), true
		}
	}
	return name, ok
}

// backfillRow lands one row's bump_counter directly (not write-behind) and
// publishes the resulting full state immediately, so sesame's projection
// (and any other replica of this service) sees the option without a
// restart — the same fan-out shape Rename uses for the same reason.
func (r *Commands) backfillRow(ctx context.Context, userID uint64, name, bumpCounter string) error {
	key := commandKey{userID: userID, name: name}
	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Commands.Update().
			Where(commands.UserIDEQ(userID), commands.NameEQ(name)).
			SetBumpCounter(bumpCounter).
			Save(ctx)
	})
	if err != nil || updated == 0 {
		return err
	}

	r.Invalidate(userID)

	states, serr := r.rowStates(ctx, []commandKey{key})
	if serr != nil {
		return serr
	}
	state, found := states[key]
	if !found {
		return nil
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, state)
}
