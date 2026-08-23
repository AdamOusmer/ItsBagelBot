// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// This file is the featured-bundle viewer: HenrikDev carries Riot's own store
// payload (prices, discounts, per-item breakdown, remaining rotation time),
// and Riot's content CDN names it. The two upstreams are joined here rather
// than at the caller because the join is what makes the endpoint useful — a
// bundle of bare UUIDs is a shop nobody can read.
//
// Why bundles and not the daily skin rotation: HenrikDev's global
// store-offers endpoint returns 404 code 46 ("Riot has removed this
// implementation") at every version — Riot killed the public rotation feed,
// and the per-player views (personal store, night market) need viewer
// credentials HenrikDev bans products for collecting. The featured bundle is
// what survives globally, and it is the piece players actually ask bots to
// announce anyway.

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

// Bundle items are joined by catalogue membership alone, not by an item-type
// allowlist: the live store payload types its five skin entries with a
// per-level UUID (e7c63390...) that matches nothing in the offers feed this
// file originally targeted — whose EquippableSkin id (e5c1dd93...) died along
// with that endpoint. Only weapon-skin level UUIDs exist in the weapons-skins
// catalogue, so "resolves" and "is a skin" are the same test; buddies, cards
// and sprays fall out on their own without us hardcoding another generation
// of Riot's type constants.

// featuredWire is HenrikDev's /valorant/v1/store-featured. Only the headline
// bundle is decoded: Bundles[] repeats the same shape for sub-bundles and
// BundleRemainingDurationInSeconds duplicates the per-bundle countdown.
type featuredWire struct {
	Data struct {
		FeaturedBundle struct {
			Bundle featuredBundle `json:"Bundle"`
		} `json:"FeaturedBundle"`
	} `json:"data"`
}

type featuredBundle struct {
	ID          string `json:"ID"`
	DataAssetID string `json:"DataAssetID"`
	// Whole-bundle discount as a fraction (0.451 = 45.1% off).
	TotalDiscountPercent float64 `json:"TotalDiscountPercent"`
	// Riot's own countdown to when this bundle leaves the store.
	DurationRemainingInSeconds float64        `json:"DurationRemainingInSeconds"`
	Items                      []featuredItem `json:"Items"`
}

// Prices arrive as JSON floats even when whole ("DiscountedPrice":0.0), so
// they decode as float64 and are truncated only at the reply boundary. A
// plain int64 field hard-fails the entire store decode on the first
// fractional zero (observed live), not merely loses precision.
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

// bundleMetaWire is the content CDN's catalogue entry for one bundle, keyed by
// the DataAssetID the store payload carries.
type bundleMetaWire struct {
	Data struct {
		UUID        string `json:"uuid"`
		DisplayName string `json:"displayName"`
		// SubText is the edition suffix ("2.0", "Collection") rendered under
		// the name in-game.
		SubText     string `json:"displayNameSubText"`
		Description string `json:"description"`
		DisplayIcon string `json:"displayIcon"`
	} `json:"data"`
}

// skinsWire is the catalogue half of the item join. One payload lists every
// skin ever shipped (a few MB); it changes only at patches, so it is cached
// under one key all pods share for a full day rather than refetched per
// request. Bundle items reference skin LEVEL uuids, not the parent skin uuid,
// so every level uuid indexes back to its skin too.
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

// tiersWire maps content tier UUIDs to their display names and rarity colours.
type tiersWire struct {
	Data []tierAsset `json:"data"`
}

type tierAsset struct {
	UUID            string `json:"uuid"`
	DisplayName     string `json:"displayName"`
	BackgroundColor string `json:"backgroundColor"`
}

// skinTTL bounds all three catalogue entries. Patches land roughly
// fortnightly; 24 hours means a patch day serves old names for at most a day
// — visible as a renamed skin keeping its old label, never as a missing or
// wrong-priced item, because prices come fresh from the store payload every
// window.
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

// bundleMeta resolves one bundle's display identity. Cached under its data
// asset UUID: the same bundle reappears on re-rerun rotations, so repeat
// features cost nothing after the first.
func (p *api) bundleMeta(ctx context.Context, dataAssetID string) (bundleMetaWire, error) {
	key := core.Key(providerName, "bundle", dataAssetID)
	return core.Cached(ctx, p.cache, key, skinTTL, negativeTTL, nil, func(ctx context.Context) (bundleMetaWire, error) {
		var meta bundleMetaWire
		if err := p.content.GetJSON(ctx, "/v1/bundles/"+url.PathEscape(dataAssetID), nil, &meta); err != nil {
			return bundleMetaWire{}, err
		}
		if strings.TrimSpace(meta.Data.DisplayName) == "" {
			// A catalogue miss here means the bundle predates or postdates the
			// cached weapons/skins-style index; without this guard the reply
			// would carry an empty name and look broken rather than unknown.
			return bundleMetaWire{}, &core.UpstreamError{Status: 404, Message: "bundle not found in catalogue"}
		}
		return meta, nil
	})
}

// shopItem is one weapon skin inside the featured bundle, ready for
// rendering. Color is the rarity's background hex straight from the content
// tiers, so the module can colour-code without owning a rarity table. Price
// is what the bundle charges for it today — the discounted price when one
// applies, the base price otherwise.
type shopItem struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Tier  string `json:"tier,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// shopReply is the answer to valorant.shop: the current featured bundle.
// ExpiresSeconds is Riot's own countdown to the next rotation, so a template
// can print an exact deadline instead of a guess. Price sums the listed skin
// items' effective prices — the bundle may also carry cards, sprays or VP,
// which render nowhere but are included in Riot's own checkout total.
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

// shopFetch joins three legs concurrently: the priced store payload from
// HenrikDev, plus the skin catalogue and rarity tiers from the content CDN.
// Each leg is independently cached (the catalogues for a day), so steady state
// costs exactly one Henrik call per window and zero content calls.
//
// Caching is a fixed six hours rather than a deadline pinned to the bundle's
// remaining seconds: the flow sizes windows before fetching, so the upstream
// countdown cannot inform them — and it does not need to, because the reply
// echoes ExpiresSeconds for exact rendering while six hours keeps any served
// copy within a rounding error of Riot's own schedule.
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
		// Unknown skins are skipped rather than rendered as blank rows: a
		// catalogue lagging a patch shows a smaller list with an honest Count,
		// never "Unknown item — 1800VP" five times.
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
