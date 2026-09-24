// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"golang.org/x/sync/errgroup"
)

type featuredWire struct {
	Data struct {
		FeaturedBundle struct {
			Bundle featuredBundle `json:"Bundle"`
		} `json:"FeaturedBundle"`
	} `json:"data"`
}

type featuredBundle struct {
	ID                         string         `json:"ID"`
	DataAssetID                string         `json:"DataAssetID"`
	TotalDiscountPercent       float64        `json:"TotalDiscountPercent"`
	DurationRemainingInSeconds float64        `json:"DurationRemainingInSeconds"`
	Items                      []featuredItem `json:"Items"`
}

// Prices arrive as JSON floats; an int64 field fails the whole store decode.
type featuredItem struct {
	Item struct {
		ItemTypeID string  `json:"ItemTypeID"`
		ItemID     string  `json:"ItemID"`
		Amount     float64 `json:"Amount"`
	} `json:"Item"`
	BasePrice       float64 `json:"BasePrice"`
	DiscountPercent float64 `json:"DiscountPercent"`
	DiscountedPrice float64 `json:"DiscountedPrice"`
}

type bundleMetaWire struct {
	Data struct {
		UUID        string `json:"uuid"`
		DisplayName string `json:"displayName"`
		SubText     string `json:"displayNameSubText"`
		Description string `json:"description"`
		DisplayIcon string `json:"displayIcon"`
	} `json:"data"`
}

type skinsWire struct {
	Data []skinAsset `json:"data"`
}

type skinAsset struct {
	UUID            string `json:"uuid"`
	DisplayName     string `json:"displayName"`
	DisplayIcon     string `json:"displayIcon"`
	ContentTierUUID string `json:"contentTierUuid"`
	Levels          []struct {
		UUID string `json:"uuid"`
	} `json:"levels"`
}

type tiersWire struct {
	Data []tierAsset `json:"data"`
}

type tierAsset struct {
	UUID            string `json:"uuid"`
	DisplayName     string `json:"displayName"`
	BackgroundColor string `json:"backgroundColor"`
}

const skinTTL = 24 * time.Hour

func (p *api) skinCatalogue(ctx context.Context) (map[string]skinAsset, error) {
	key := core.Key(providerName, "skins", "catalogue")
	return core.Cached(ctx, p.cache, key, skinTTL, negativeTTL, nil, func(ctx context.Context) (map[string]skinAsset, error) {
		var wire skinsWire
		if err := p.content.GetJSON(ctx, "/v1/weapons/skins", nil, &wire); err != nil {
			return nil, err
		}
		catalogue := make(map[string]skinAsset, len(wire.Data)*4)
		for _, asset := range wire.Data {
			catalogue[asset.UUID] = asset
			for _, level := range asset.Levels {
				catalogue[level.UUID] = asset
			}
		}
		return catalogue, nil
	})
}

func (p *api) contentTiers(ctx context.Context) (map[string]tierAsset, error) {
	key := core.Key(providerName, "tiers", "content")
	return core.Cached(ctx, p.cache, key, skinTTL, negativeTTL, nil, func(ctx context.Context) (map[string]tierAsset, error) {
		var wire tiersWire
		if err := p.content.GetJSON(ctx, "/v1/contenttiers", nil, &wire); err != nil {
			return nil, err
		}
		tiers := make(map[string]tierAsset, len(wire.Data))
		for _, asset := range wire.Data {
			tiers[asset.UUID] = asset
		}
		return tiers, nil
	})
}

func (p *api) bundleMeta(ctx context.Context, dataAssetID string) (bundleMetaWire, error) {
	key := core.Key(providerName, "bundle", dataAssetID)
	return core.Cached(ctx, p.cache, key, skinTTL, negativeTTL, nil, func(ctx context.Context) (bundleMetaWire, error) {
		var meta bundleMetaWire
		if err := p.content.GetJSON(ctx, "/v1/bundles/"+url.PathEscape(dataAssetID), nil, &meta); err != nil {
			return bundleMetaWire{}, err
		}
		if strings.TrimSpace(meta.Data.DisplayName) == "" {
			return bundleMetaWire{}, &core.UpstreamError{Status: 404, Message: "bundle not found in catalogue"}
		}
		return meta, nil
	})
}

type shopItem struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Tier  string `json:"tier,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type shopReply struct {
	Bundle         string     `json:"bundle"`
	Subtitle       string     `json:"subtitle,omitempty"`
	Description    string     `json:"description,omitempty"`
	Icon           string     `json:"icon,omitempty"`
	DiscountPct    float64    `json:"discount_pct,omitempty"`
	Price          int64      `json:"price"`
	ExpiresSeconds int64      `json:"expires_in_seconds"`
	Items          []shopItem `json:"items"`
	Count          int        `json:"count"`
	Error          string     `json:"error,omitempty"`
}

func (p *api) shopFetch(ctx context.Context, _ gossiprpc.Request, _ provider.ID) (any, error) {
	var wire featuredWire
	var meta bundleMetaWire
	var catalogue map[string]skinAsset
	var tiers map[string]tierAsset

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		if err := p.http.GetJSON(groupCtx, "/valorant/v1/store-featured", nil, &wire); err != nil {
			return err
		}
		var err error
		meta, err = p.bundleMeta(groupCtx, wire.Data.FeaturedBundle.Bundle.DataAssetID)
		return err
	})
	group.Go(func() error {
		var err error
		catalogue, err = p.skinCatalogue(groupCtx)
		return err
	})
	group.Go(func() error {
		var err error
		tiers, err = p.contentTiers(groupCtx)
		return err
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}

	bundle := wire.Data.FeaturedBundle.Bundle
	items := make([]shopItem, 0, len(bundle.Items))
	var price int64
	for _, entry := range bundle.Items {
		skin, ok := catalogue[entry.Item.ItemID]
		if !ok {
			continue
		}
		effective := entry.BasePrice
		if entry.DiscountedPrice > 0 {
			effective = entry.DiscountedPrice
		}
		item := shopItem{Name: skin.DisplayName, Price: int64(effective), Icon: skin.DisplayIcon}
		if tier, ok := tiers[skin.ContentTierUUID]; ok {
			item.Tier = tier.DisplayName
			item.Color = tier.BackgroundColor
		}
		items = append(items, item)
		price += int64(effective)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Price > items[j].Price })
	return shopReply{
		Bundle:         meta.Data.DisplayName,
		Subtitle:       meta.Data.SubText,
		Description:    meta.Data.Description,
		Icon:           meta.Data.DisplayIcon,
		DiscountPct:    bundle.TotalDiscountPercent * 100,
		Price:          price,
		ExpiresSeconds: int64(bundle.DurationRemainingInSeconds),
		Items:          items,
		Count:          len(items),
	}, nil
}
