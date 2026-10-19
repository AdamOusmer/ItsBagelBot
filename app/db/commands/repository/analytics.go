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
		state := "inactive"
		if c.IsActive {
			state = "active"
		}
		tally.Add(catalog.CustomTotal, state)
		tally.Add(catalog.CustomPerm, c.Perm)
		addFlags(tally, c)
	}
	for range channels {
		tally.Add(catalog.CustomChannels)
	}
	return tally
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

func distinctUsers(items []commandKey) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.userID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}
