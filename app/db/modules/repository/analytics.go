// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/pkg/db"
)

func (r *Modules) SetAnalytics(rec *analytics.Recorder) {
	r.analytics = rec
}

func moduleTally(rows []*ent.Modules) analytics.Tally {
	tally := analytics.Tally{}
	enabled := map[uint64]int{}
	for _, m := range rows {
		enabled[m.UserID] += 0
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
		tally.Add(catalog.ModulesBreadth, strconv.Itoa(n))
	}
	return tally
}

func (r *Modules) AnalyticsSeed(ctx context.Context) (analytics.Tally, error) {
	tally := analytics.Tally{}
	for _, name := range catalog.ModuleNames() {
		if catalog.ModuleEnabledByDefault(name) {
			tally.Add(catalog.ModulesDefaults, name)
		}
	}
	if err := r.seedModules(ctx, tally); err != nil {
		return nil, err
	}
	if err := r.seedRecipes(ctx, tally); err != nil {
		return nil, err
	}
	if err := r.seedSpotify(ctx, tally); err != nil {
		return nil, err
	}
	if err := r.seedGovee(ctx, tally); err != nil {
		return nil, err
	}
	return tally, nil
}

func (r *Modules) seedModules(ctx context.Context, tally analytics.Tally) error {
	var user uint64
	seen := false
	enabled := 0
	flush := func() {
		if seen {
			tally.Add(catalog.ModulesBreadth, strconv.Itoa(enabled))
		}
		enabled = 0
	}
	err := analytics.Pages(ctx,
		func(ctx context.Context, after *moduleKey, limit int) ([]*ent.Modules, error) {
			return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Modules, error) {
				q := r.client.Modules.Query().
					Order(modules.ByUserID(), modules.ByName()).
					Select(modules.FieldUserID, modules.FieldName, modules.FieldIsEnabled, modules.FieldConfigs).
					Limit(limit)
				if after != nil {
					q = q.Where(modules.Or(
						modules.UserIDGT(after.userID),
						modules.And(modules.UserIDEQ(after.userID), modules.NameGT(after.name)),
					))
				}
				return q.All(ctx)
			})
		},
		func(m *ent.Modules) moduleKey { return moduleKey{userID: m.UserID, name: m.Name} },
		func(m *ent.Modules) {
			if !seen || m.UserID != user {
				flush()
				user = m.UserID
				seen = true
			}
			if !catalog.KnownModule(m.Name) {
				return
			}
			state := "off"
			if m.IsEnabled {
				state = "on"
				enabled++
			}
			tally.Add(catalog.ModulesRows, m.Name, state)
			tallyTimers(tally, m)
		})
	flush()
	return err
}

func (r *Modules) seedSpotify(ctx context.Context, tally analytics.Tally) error {
	return analytics.Pages(ctx,
		func(ctx context.Context, after *uint64, limit int) ([]*ent.SpotifyCredential, error) {
			return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.SpotifyCredential, error) {
				q := r.client.SpotifyCredential.Query().
					Order(spotifycredential.ByUserID()).
					Select(spotifycredential.FieldUserID, spotifycredential.FieldTokenEnc).
					Limit(limit)
				if after != nil {
					q = q.Where(spotifycredential.UserIDGT(*after))
				}
				return q.All(ctx)
			})
		},
		func(c *ent.SpotifyCredential) uint64 { return c.UserID },
		func(c *ent.SpotifyCredential) {
			if len(c.TokenEnc) > 0 {
				tally.Add(catalog.IntegrationsConnected, "spotify")
			}
		})
}

func (r *Modules) seedGovee(ctx context.Context, tally analytics.Tally) error {
	n, err := db.WithQuery(ctx, r.client.GoveeCredential.Query().Count)
	tally.AddN(int64(n), catalog.IntegrationsConnected, "govee")
	return err
}
