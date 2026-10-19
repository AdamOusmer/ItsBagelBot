// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"slices"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/fetchkey"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

func (r *Commands) SetAnalytics(rec *analytics.Recorder) { r.analytics = rec }

func (r *Fetches) SetAnalytics(rec *analytics.Recorder) { r.analytics = rec }

// track reads the channels' command shapes before a write and returns the step that
// shifts the running totals once it lands. Only shape columns are read, never name or response.
func (r *Commands) track(ctx context.Context, ids ...uint64) func() {
	return trackShift(r.analytics, func() (analytics.Tally, error) { return r.commandState(ctx, ids) })
}

func (r *Fetches) trackKeys(ctx context.Context, ids ...uint64) func() {
	return trackShift(r.analytics, func() (analytics.Tally, error) { return r.keyState(ctx, ids) })
}

func trackShift(rec *analytics.Recorder, state func() (analytics.Tally, error)) func() {
	if rec == nil {
		return func() {}
	}
	before, err := state()
	if err != nil {
		return func() {}
	}
	return func() {
		if after, err := state(); err == nil {
			rec.Shift(before, after)
		}
	}
}

func (r *Commands) commandState(ctx context.Context, ids []uint64) (analytics.Tally, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().
			Where(commands.UserIDIn(ids...)).
			Select(
				commands.FieldUserID, commands.FieldIsActive, commands.FieldPerm,
				commands.FieldStreamOnlineOnly, commands.FieldCooldown, commands.FieldUserCooldown,
				commands.FieldBumpCounter, commands.FieldAliases,
			).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	return commandTally(rows), nil
}

func commandTally(rows []*ent.Commands) analytics.Tally {
	tally := analytics.Tally{}
	channels := map[uint64]struct{}{}
	for _, c := range rows {
		channels[c.UserID] = struct{}{}
		addCommand(tally, c)
	}
	for range channels {
		tally.Add(catalog.CustomChannels)
	}
	return tally
}

func addCommand(tally analytics.Tally, c *ent.Commands) {
	state := "inactive"
	if c.IsActive {
		state = "active"
	}
	tally.Add(catalog.CustomTotal, state)
	tally.Add(catalog.CustomPerm, c.Perm)
	addFlags(tally, c)
}

type commandCursor struct {
	userID uint64
	name   string
}

// AnalyticsSeed tallies every command by keyset pages over (user_id, name); response is never read.
func (r *Commands) AnalyticsSeed(ctx context.Context) (analytics.Tally, error) {
	return seedPages(ctx, r.commandPage, func(c *ent.Commands) commandCursor {
		return commandCursor{c.UserID, c.Name}
	}, func(t analytics.Tally, channels *channelEdge, c *ent.Commands) {
		channels.add(t, c.UserID, catalog.CustomChannels)
		addCommand(t, c)
	})
}

func seedPages[R, C any](ctx context.Context, page func(context.Context, *C, int) ([]R, error), cursor func(R) C, visit func(analytics.Tally, *channelEdge, R)) (analytics.Tally, error) {
	tally := analytics.Tally{}
	channels := &channelEdge{}
	err := analytics.Pages(ctx, page, cursor, func(row R) { visit(tally, channels, row) })
	return tally, err
}

// channelEdge counts a channel once while keyset pages deliver its rows together.
type channelEdge struct{ last uint64 }

func (e *channelEdge) add(tally analytics.Tally, userID uint64, m catalog.Metric, dims ...string) {
	if userID == e.last {
		return
	}
	e.last = userID
	tally.Add(m, dims...)
}

func (r *Commands) commandPage(ctx context.Context, after *commandCursor, limit int) ([]*ent.Commands, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		q := r.client.Commands.Query()
		if after != nil {
			q = q.Where(commands.Or(
				commands.UserIDGT(after.userID),
				commands.And(commands.UserID(after.userID), commands.NameGT(after.name)),
			))
		}
		return q.Order(ent.Asc(commands.FieldUserID, commands.FieldName)).
			Limit(limit).
			Select(
				commands.FieldUserID, commands.FieldName, commands.FieldIsActive, commands.FieldPerm,
				commands.FieldStreamOnlineOnly, commands.FieldCooldown, commands.FieldUserCooldown,
				commands.FieldBumpCounter, commands.FieldAliases,
			).
			All(ctx)
	})
}

type keyCursor struct {
	userID uint64
	label  string
}

// AnalyticsSeed counts users with at least one fetch key by keyset pages; key material is never read.
func (f *Fetches) AnalyticsSeed(ctx context.Context) (analytics.Tally, error) {
	return seedPages(ctx, f.keyPage, func(k *ent.FetchKey) keyCursor {
		return keyCursor{k.UserID, k.Label}
	}, func(t analytics.Tally, channels *channelEdge, k *ent.FetchKey) {
		channels.add(t, k.UserID, catalog.IntegrationsConnected, "fetch_key")
	})
}

func (f *Fetches) keyPage(ctx context.Context, after *keyCursor, limit int) ([]*ent.FetchKey, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.FetchKey, error) {
		q := f.client.FetchKey.Query()
		if after != nil {
			q = q.Where(fetchkey.Or(
				fetchkey.UserIDGT(after.userID),
				fetchkey.And(fetchkey.UserID(after.userID), fetchkey.LabelGT(after.label)),
			))
		}
		return q.Order(ent.Asc(fetchkey.FieldUserID, fetchkey.FieldLabel)).
			Limit(limit).
			Select(fetchkey.FieldUserID, fetchkey.FieldLabel).
			All(ctx)
	})
}

// SeedAll merges the command and fetch-key totals into one seed.
func SeedAll(r *Commands, f *Fetches) analytics.Seed {
	return func(ctx context.Context) (analytics.Tally, error) {
		total, err := r.AnalyticsSeed(ctx)
		if err != nil {
			return nil, err
		}
		keys, err := f.AnalyticsSeed(ctx)
		if err != nil {
			return nil, err
		}
		for field, n := range keys {
			total[field] += n
		}
		return total, nil
	}
}

func addFlags(tally analytics.Tally, c *ent.Commands) {
	flags := []struct {
		name string
		on   bool
	}{
		{"online_only", c.StreamOnlineOnly},
		{"aliased", len(c.Aliases) > 0},
		{"cooldown", c.Cooldown > 0 || c.UserCooldown > 0},
		{"counter", c.BumpCounter != ""},
	}
	for _, f := range flags {
		if f.on {
			tally.Add(catalog.CustomFlags, f.name)
		}
	}
}

func (r *Fetches) keyState(ctx context.Context, ids []uint64) (analytics.Tally, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.FetchKey, error) {
		return r.client.FetchKey.Query().
			Where(fetchkey.UserIDIn(ids...)).
			Select(fetchkey.FieldUserID).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	channels := map[uint64]struct{}{}
	for _, k := range rows {
		channels[k.UserID] = struct{}{}
	}
	tally := analytics.Tally{}
	for range channels {
		tally.Add(catalog.IntegrationsConnected, "fetch_key")
	}
	return tally, nil
}

func commandKeys(items []data.CommandChangedDTO) []commandKey {
	keys := make([]commandKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, commandKey{userID: item.UserID, name: item.Name})
	}
	return keys
}

func distinctUsers(items []commandKey) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.userID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}
