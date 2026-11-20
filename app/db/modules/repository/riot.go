// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/riotlink"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

type RiotLink struct {
	Puuid    string
	GameName string
	TagLine  string
	Shard    string
	LinkedAt time.Time
}

type RiotLinks struct {
	client *ent.Client
}

func NewRiotLinks(client *ent.Client) *RiotLinks {
	return &RiotLinks{client: client}
}

func (r *RiotLinks) Set(ctx context.Context, userID uint64, link RiotLink) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if strings.TrimSpace(link.Puuid) == "" || strings.TrimSpace(link.Shard) == "" {
		return errors.New("riot link needs a puuid and a shard")
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.RiotLink.Create().
			SetUserID(userID).
			SetPuuid(link.Puuid).
			SetGameName(link.GameName).
			SetTagLine(link.TagLine).
			SetShard(strings.ToLower(link.Shard)).
			OnConflictColumns(riotlink.FieldUserID).
			UpdateNewValues().
			Exec(ctx)
	})
}

func (r *RiotLinks) Clear(ctx context.Context, userID uint64) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.RiotLink.Delete().Where(riotlink.UserIDEQ(userID)).Exec(ctx)
		return err
	})
}

func (r *RiotLinks) Get(ctx context.Context, userID uint64) (RiotLink, bool, error) {
	if err := validate.UserID(userID); err != nil {
		return RiotLink{}, false, err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.RiotLink, error) {
		return r.client.RiotLink.Query().Where(riotlink.UserIDEQ(userID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return RiotLink{}, false, nil
	}
	if err != nil {
		return RiotLink{}, false, err
	}
	return RiotLink{
		Puuid: row.Puuid, GameName: row.GameName, TagLine: row.TagLine,
		Shard: row.Shard, LinkedAt: row.UpdatedAt,
	}, true, nil
}
