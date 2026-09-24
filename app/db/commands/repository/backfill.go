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

const bumpCounterBackfillName = "bump_counter_from_counter_token"

// Writes must land directly, not through the batcher, before the marker row is recorded.
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

func bumpCounterFromResponse(response, cmdName string, log *zap.Logger) (name string, ok bool) {
	for _, tok := range tmpl.Lex(response) {
		if !isBareCounterSpan(tok) {
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

func isBareCounterSpan(tok tmpl.Token) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == "counter" && tok.HasPayload
}

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
