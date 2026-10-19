// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"slices"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/goveecredential"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

func (r *Modules) SetAnalytics(rec *analytics.Recorder) {
	r.analytics = rec
	if r.govee != nil {
		r.govee.analytics = rec
	}
	if r.spotify != nil {
		r.spotify.analytics = rec
	}
}

// track reads the channels' stored module rows before a write and returns the step that
// shifts the running totals once it lands. A default-on module with no row is absent,
// so its enabled count is accounts minus its "off" rows.
func (r *Modules) track(ctx context.Context, ids ...uint64) func() {
	return trackShift(r.analytics, func() (analytics.Tally, error) { return r.moduleState(ctx, ids) })
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

func (r *Modules) moduleState(ctx context.Context, ids []uint64) (analytics.Tally, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Modules, error) {
		return r.client.Modules.Query().
			Where(modules.UserIDIn(ids...)).
			Select(modules.FieldUserID, modules.FieldName, modules.FieldIsEnabled).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	return moduleTally(rows), nil
}

func moduleTally(rows []*ent.Modules) analytics.Tally {
	tally := analytics.Tally{}
	enabled := map[uint64]int{}
	for _, m := range rows {
		if !catalog.KnownModule(m.Name) {
			continue
		}
		state := "off"
		if m.IsEnabled {
			state = "on"
			enabled[m.UserID]++
		}
		tally.Add(catalog.ModulesRows, m.Name, state)
	}
	for _, n := range enabled {
		tally.Add(catalog.ModulesBreadth, breadthBucket(n))
	}
	return tally
}

func breadthBucket(n int) string {
	switch {
	case n <= 1:
		return "1"
	case n == 2:
		return "2"
	case n <= 5:
		return "3-5"
	default:
		return "6+"
	}
}

func changedUsers(items []data.ModuleChangedDTO) []uint64 {
	ids := make([]uint64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (g *GoveeCreds) track(ctx context.Context, userID uint64) func() {
	return trackShift(g.analytics, func() (analytics.Tally, error) {
		n, err := db.WithQuery(ctx, g.client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(userID)).Count)
		return linkTally("govee", n > 0), err
	})
}

func (s *SpotifyCreds) track(ctx context.Context, userID uint64) func() {
	return trackShift(s.analytics, func() (analytics.Tally, error) {
		n, err := db.WithQuery(ctx, s.client.SpotifyCredential.Query().
			Where(spotifycredential.UserIDEQ(userID), spotifycredential.TokenEncNotNil()).Count)
		return linkTally("spotify", n > 0), err
	})
}

// trackLinks covers account deletion, which removes credential rows inside its own transaction.
func (r *Modules) trackLinks(ctx context.Context, userID uint64) func() {
	shifts := []func(){}
	if r.govee != nil {
		shifts = append(shifts, r.govee.track(ctx, userID))
	}
	if r.spotify != nil {
		shifts = append(shifts, r.spotify.track(ctx, userID))
	}
	return func() {
		for _, shift := range shifts {
			shift()
		}
	}
}

func linkTally(link string, connected bool) analytics.Tally {
	tally := analytics.Tally{}
	if connected {
		tally.Add(catalog.IntegrationsConnected, link)
	}
	return tally
}
