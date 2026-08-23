// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// This file is the daily offer rotation: HenrikDev prices item UUIDs, Riot's
// content CDN names them. The two upstreams are joined here rather than at the
// caller because the join is what makes the endpoint useful — a rotation of
// bare UUIDs is a shop nobody can read.
//
// What is intentionally absent: personal shops and night markets need the
// viewer's own Riot credentials, which HenrikDev bans products for collecting
// (see the package comment). Bundles are also out: store-featured carries its
// own bundle metadata and deserves its own endpoint when someone wants it,
// not a half-join bolted onto this one.

package valorant

import (
	"context"
	"sort"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"golang.org/x/sync/errgroup"
)

// These two UUIDs are schema-level constants of the game's economy model, not
// data that rotates: every skin offer prices itself in VP under the first and
// identifies its reward kind under the second. They have been stable since
// open beta (they are how SkinPeek, ValoDex and every community shop tracker
// decode the same payload), so hardcoding them trades one config knob nobody
// would ever set correctly for constants nothing has ever shaken.
const (
	valorantPointsTypeID = "85ad13f7-3d1b-5128-9eb2-7cd8ee0b5741"
	skinItemTypeID       = "e5c1dd93-8875-4a96-8f30-6a9b17da1ce4"
)

// The shop flips over at 20:00 America/Los_Angeles — 03:00 UTC during PDT.
// Winter PST pushes that to 04:00 UTC, so the day after the clocks change the
// window runs an hour long and stale-while-revalidate covers the gap: the
// half-window sizing in the flow means the last pre-flip build expires right
// on 03:00, and the first read past it refreshes against the same deadline
// instead of serving yesterday's rotation from the physical tail. Computing
// "next 20:00 Pacific" exactly needs a location database on every pod for a
// boundary that self-heals anyway; 03:00 fixed is the deliberate trade.
func nextShopRotation(now time.Time) time.Time {
	today := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, time.UTC)
	if !now.Before(today) {
		return today.Add(24 * time.Hour)
	}
	return today
}

// offersWire is the priced half of the join. Cost keys currency type UUIDs to
// amounts; Rewards carries what the offer actually grants, filtered down to
// skins below so agent-contract points and radianite never masquerade as shop
// rows.
type offersWire struct {
	Data struct {
		Offers []offerEntry `json:"Offers"`
	} `json:"data"`
}

type offerEntry struct {
	OfferID string           `json:"OfferID"`
	Cost    map[string]int64 `json:"Cost"`
	Rewards []offerReward    `json:"Rewards"`
}

type offerReward struct {
	ItemID     string `json:"ItemID"`
	ItemTypeID string `json:"ItemTypeID"`
}

// skinsWire is the catalogue half. One payload lists every skin ever shipped
// (a few MB); it changes only at patches, so it is cached under one key all
// pods share for a full day rather than refetched per request.
type skinsWire struct {
	Data []skinAsset `json:"data"`
}

type skinAsset struct {
	UUID            string `json:"uuid"`
	DisplayName     string `json:"displayName"`
	DisplayIcon     string `json:"displayIcon"`
	ContentTierUUID string `json:"contentTierUuid"`
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

// skinTTL bounds both catalogue entries. Patches land roughly fortnightly;
// 24 hours means a patch day serves old names for at most a day — visible as
// a renamed skin keeping its old label, never as a missing or wrong-priced
// item, because prices come fresh from the offers leg every window.
const skinTTL = 24 * time.Hour

func (p *api) skinCatalogue(ctx context.Context) (map[string]skinAsset, error) {
	key := core.Key(providerName, "skins", "catalogue")
	return core.Cached(ctx, p.cache, key, skinTTL, negativeTTL, nil, func(ctx context.Context) (map[string]skinAsset, error) {
		var wire skinsWire
		if err := p.content.GetJSON(ctx, "/v1/weapons/skins", nil, &wire); err != nil {
			return nil, err
		}
		catalogue := make(map[string]skinAsset, len(wire.Data))
		for _, asset := range wire.Data {
			catalogue[asset.UUID] = asset
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

// shopItem is one rotated skin ready for rendering. Color is the rarity's
// background hex ("#0f1923"-style) straight from the content tiers, so the
// module can colour-code without owning a rarity table.
type shopItem struct {
	Name  string `json:"name"`
	Price int64  `json:"price"`
	Tier  string `json:"tier,omitempty"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

// shopReply is the answer to valorant.shop: today's global skin rotation.
// ResetUnix is the instant the rotation turns over, so a template can print a
// countdown. Empty is true on the rare days Riot rotates nothing — a normal
// answer, not an error.
type shopReply struct {
	ResetUnix int64      `json:"reset_unix"`
	Items     []shopItem `json:"items"`
	Count     int        `json:"count"`
	Empty     bool       `json:"empty"`
	Error     string     `json:"error,omitempty"`
}

// shopFetch joins three legs concurrently: prices from HenrikDev, the skin
// catalogue and rarity tiers from the content CDN. Each leg is independently
// cached (the catalogues for a day), so steady state costs exactly one Henrik
// call per rotation window and zero content calls.
func (p *api) shopFetch(ctx context.Context, _ gossiprpc.Request, _ provider.ID) (any, error) {
	var wire offersWire
	var catalogue map[string]skinAsset
	var tiers map[string]tierAsset

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return p.http.GetJSON(groupCtx, "/valorant/v1/store-offers", nil, &wire)
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

	items := make([]shopItem, 0, len(wire.Data.Offers))
	for _, offer := range wire.Data.Offers {
		if !isSkinOffer(offer) {
			continue
		}
		price := offer.Cost[valorantPointsTypeID]
		skin, ok := catalogue[offer.OfferID]
		// Unknown skins are skipped rather than rendered as blank rows. The
		// catalogue lags a new act's items by at most a patch cycle; a rowless
		// shop that day reads as "smaller rotation", which is true-ish, while
		// "Unknown item — 1800VP" five times reads as breakage.
		if !ok || price <= 0 {
			continue
		}
		item := shopItem{Name: skin.DisplayName, Price: price, Icon: skin.DisplayIcon}
		if tier, ok := tiers[skin.ContentTierUUID]; ok {
			item.Tier = tier.DisplayName
			item.Color = tier.BackgroundColor
		}
		items = append(items, item)
	}
	// Priciest first: the rotation is read top-down as "what's worth looking
	// at", and Ultra editions bury cheap accessories when left in upstream
	// offer order.
	sort.SliceStable(items, func(i, j int) bool { return items[i].Price > items[j].Price })
	return shopReply{
		ResetUnix: nextShopRotation(time.Now()).Unix(),
		Items:     items,
		Count:     len(items),
		Empty:     len(items) == 0,
	}, nil
}

// isSkinOffer filters to single-skin direct purchases. Bundle offers carry the
// same shape with a bundle-level reward set; they are excluded here because
// their OfferID is not in the weapons-skins catalogue and their pricing model
// (bundle discount over sum-of-parts) is its own feature.
func isSkinOffer(offer offerEntry) bool {
	if len(offer.Rewards) != 1 {
		return false
	}
	return strings.EqualFold(offer.Rewards[0].ItemTypeID, skinItemTypeID)
}
