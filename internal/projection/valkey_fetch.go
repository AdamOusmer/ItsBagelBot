// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const fetchFieldPrefix = "fetch:"

const fetchesMarkerField = "fetches:projected"

type FetchView struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
}

func (v *Store) SetFetch(ctx context.Context, dto data.FetchChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	field := fetchFieldPrefix + strings.ToLower(dto.Name)

	if dto.Deleted {
		cmds := append(
			[]valkey.Completed{pkg_valkey.HDelCmd(v.client, key, field)},
			v.expiryCommands(key, DefaultTTL)...,
		)
		return v.pipeline(ctx, cmds...)
	}

	body, err := codec.Marshal(FetchView{
		Name:     strings.ToLower(dto.Name),
		URL:      dto.URL,
		JSONPath: dto.JSONPath,
		KeyLabel: dto.KeyLabel,
		IsActive: dto.IsActive,
	})
	if err != nil {
		return err
	}

	cmds := append(
		[]valkey.Completed{
			pkg_valkey.HSetCmd(v.client, key, pkg_valkey.HashFieldValue{Field: field, Value: string(body)}),
		},
		v.expiryCommands(key, DefaultTTL)...,
	)
	return v.pipeline(ctx, cmds...)
}

func (v *Store) SetFetches(ctx context.Context, userID uint64, fetches []FetchView) error {
	return v.SetFetchesWithTTL(ctx, userID, fetches, DefaultTTL)
}

func (v *Store) SetFetchesWithTTL(ctx context.Context, userID uint64, fetches []FetchView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	rows := make([][2]string, 0, len(fetches))
	for _, f := range fetches {
		body, err := codec.Marshal(f)
		if err != nil {
			return err
		}
		rows = append(rows, [2]string{fetchFieldPrefix + strings.ToLower(f.Name), string(body)})
	}
	return v.replaceSection(ctx, userID, sectionWrite{
		prefixes: []string{fetchFieldPrefix},
		marker:   fetchesMarkerField,
		ttl:      ttl,
		rows:     rows,
	})
}

func (v *Store) GetFetch(ctx context.Context, userID uint64, name string) (view FetchView, found bool, projected bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	lname := strings.ToLower(name)

	res := pkg_valkey.ExecMulti(ctx, v.client,
		pkg_valkey.HGetCmd(v.client, key, fetchFieldPrefix+lname),
		pkg_valkey.HGetCmd(v.client, key, fetchesMarkerField),
	)

	projected, err = markerProjected(res[1])
	if err != nil {
		return FetchView{}, false, false, err
	}

	view, found, err = decodeJSONField[FetchView](res[0])
	if err != nil {
		return FetchView{}, false, projected, err
	}
	return view, found, projected, nil
}

var fetchesSection = sectionRead{prefix: fetchFieldPrefix, marker: fetchesMarkerField}

func (v *Store) GetFetches(ctx context.Context, userID uint64) ([]FetchView, bool, error) {
	return getSection[FetchView](ctx, v, userID, fetchesSection)
}
