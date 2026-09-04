// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package providers wires every external API system gossip serves, the
// twin of sesame's app/twitch/sesame/modules package: each system lives in its own
// subpackage, and adding one is writing that package plus one entry here.
package providers

import (
	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/clashroyale"
	"ItsBagelBot/app/gossip/internal/providers/custom"
	"ItsBagelBot/app/gossip/internal/providers/fortnite"
	"ItsBagelBot/app/gossip/internal/providers/govee"
	"ItsBagelBot/app/gossip/internal/providers/hypixel"
	"ItsBagelBot/app/gossip/internal/providers/mcsr"
	"ItsBagelBot/app/gossip/internal/providers/paceman"
	"ItsBagelBot/app/gossip/internal/providers/spotify"
	"ItsBagelBot/app/gossip/internal/providers/urchin"
	"ItsBagelBot/app/gossip/internal/providers/valorant"

	"go.uber.org/zap"
)

// All builds every configured provider, in registration order. Each append*
// helper adds its provider when configured or logs why it is skipped; a skipped
// provider's subjects simply time out at the caller, the same failure mode as
// the upstream being down.
func All(cfg *config.Config, d provider.Deps) []provider.Provider {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	var out []provider.Provider
	out = appendUrchin(out, cfg, d, log)
	out = appendHypixel(out, cfg, d, log)
	out = appendMcsr(out, cfg, d, log)
	out = appendPaceman(out, cfg, d, log)
	out = appendFortnite(out, cfg, d, log)
	out = appendGovee(out, cfg, d, log)
	out = appendClashRoyale(out, cfg, d, log)
	out = appendValorant(out, cfg, d, log)
	out = appendSpotify(out, cfg, d, log)
	out = appendCustom(out, cfg, d, log)
	return out
}

// gated registers one provider behind its single disable condition: when the
// gate trips, log why and leave out unchanged (a skipped provider's subjects
// simply time out at the caller, the same failure mode as the upstream being
// down); otherwise run the constructor lazily and append it. Factoring this
// once means adding a provider with the same "one credential/flag gates it"
// shape is writing a flag expression, a skip reason and an env-to-Config
// mapping below — not a seventh copy of the whole sequence (the duplication
// CodeScene flagged once appendPaceman made the third near-identical copy of
// what began as an inline if/warn/append in every helper).
func gated[T any](out []provider.Provider, log *zap.Logger, disabled bool, skipReason string, build func(T, provider.Deps) provider.Provider, cfg T, d provider.Deps) []provider.Provider {
	if disabled {
		log.Warn(skipReason)
		return out
	}
	return append(out, build(cfg, d))
}

func appendUrchin(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.UrchinAPIKey == "", "urchin provider disabled: URCHIN_API_KEY not set", urchin.New, urchin.Config{
		BaseURL:   cfg.UrchinBaseURL,
		APIKey:    cfg.UrchinAPIKey,
		RateLimit: cfg.UrchinRateLimit,
	}, d)
}

func appendHypixel(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.HypixelAPIKey == "", "hypixel provider disabled: HYPIXEL_API_KEY not set (!bwstats will not answer)", hypixel.New, hypixel.Config{
		BaseURL:         cfg.HypixelBaseURL,
		MojangBaseURL:   cfg.MojangBaseURL,
		APIKey:          cfg.HypixelAPIKey,
		RateLimit:       cfg.HypixelRateLimit,
		MojangRateLimit: cfg.MojangRateLimit,
	}, d)
}

func appendMcsr(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, !cfg.McsrEnabled, "mcsr provider disabled: MCSR_ENABLED=false", mcsr.New, mcsr.Config{
		BaseURL:   cfg.McsrBaseURL,
		APIKey:    cfg.McsrAPIKey,
		RateLimit: cfg.McsrRateLimit,
	}, d)
}

// appendPaceman adds the paceman provider. Its public API needs no key, so
// unlike the credential-gated providers there is nothing to gate on — the
// only switch is the operator-controlled PacemanEnabled kill switch.
func appendPaceman(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, !cfg.PacemanEnabled, "paceman provider disabled: PACEMAN_ENABLED=false", paceman.New, paceman.Config{
		BaseURL:     cfg.PacemanBaseURL,
		UserBaseURL: cfg.PacemanUserBaseURL,
		RateLimit:   cfg.PacemanRateLimit,
	}, d)
}

// appendFortnite adds the fortnite provider behind the FORTNITE_ENABLED flag
// (dark until tested). The api-fortnite.com key gates only the stats
// endpoint: the shop upstream (fortnite-api.com) is public, so a keyless
// provider still answers !store and merely skips !fnstats (shop-only mode) —
// a soft warning alongside construction, not a skip, so it does not fit
// appendIf's single hard gate.
func appendFortnite(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if !cfg.FortniteEnabled {
		log.Warn("fortnite provider disabled: FORTNITE_ENABLED=false")
		return out
	}
	if cfg.FortniteAPIKey == "" {
		log.Warn("fortnite provider running shop-only: FORTNITE_API_KEY not set (!fnstats will not answer)")
	}
	return append(out, fortnite.New(fortnite.Config{
		ShopBaseURL:     cfg.FortniteBaseURL,
		StatsBaseURL:    cfg.FortniteStatsBaseURL,
		APIKey:          cfg.FortniteAPIKey,
		ShopRateLimit:   cfg.FortniteRateLimit,
		StatsRateLimit:  cfg.FortniteStatsRateLimit,
		SeasonStartUnix: cfg.FortniteSeasonStart,
	}, d))
}

// appendGovee adds the govee provider. It needs no service key — each
// broadcaster brings their own — but it does need the key resolver to fetch
// them; without it (the modules internal key RPC unwired) there is nothing to
// authenticate with, so it is skipped like any credential-less provider.
func appendGovee(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.GoveeKeys == nil, "govee provider disabled: no key resolver (modules govee RPC unwired)", govee.New, govee.Config{
		BaseURL:   cfg.GoveeBaseURL,
		RateLimit: cfg.GoveeRateLimit,
	}, d)
}

// appendClashRoyale adds the Clash Royale provider behind its RoyaleAPI proxy
// token, the same credential gate as urchin/hypixel. The key itself is created
// on developer.clashroyale.com (Supercell) with RoyaleAPI's proxy egress IP
// 45.79.218.79 whitelisted on it; the proxy then forwards Bearer-keyed calls
// to api.clashroyale.com. A key in Doppler lights all four !cr commands.
func appendClashRoyale(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.ClashRoyaleAPIKey == "", "clashroyale provider disabled: CLASHROYALE_API_KEY not set (!cr commands will not answer)", clashroyale.New, clashroyale.Config{
		BaseURL:   cfg.ClashRoyaleBaseURL,
		APIKey:    cfg.ClashRoyaleAPIKey,
		RateLimit: cfg.ClashRoyaleRateLimit,
	}, d)
}

// appendValorant adds the Valorant provider behind its HenrikDev key, the same
// credential gate as urchin/hypixel/clashroyale. The key gates everything:
// unlike fortnite there is no keyless fallback mode — even the featured-bundle
// viewer prices itself through HenrikDev (only its name/icon join rides the
// keyless content CDN), so a missing key leaves every !val command dark.
func appendValorant(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.ValorantAPIKey == "", "valorant provider disabled: VALORANT_API_KEY not set (!val commands will not answer)", valorant.New, valorant.Config{
		BaseURL:          cfg.ValorantBaseURL,
		ContentBaseURL:   cfg.ValorantContentBaseURL,
		APIKey:           cfg.ValorantAPIKey,
		RateLimit:        cfg.ValorantRateLimit,
		ContentRateLimit: cfg.ValorantContentRateLimit,
	}, d)
}

// appendSpotify adds the Spotify music provider. Like govee it needs no
// service key of its own, and since the fleet retired its shared Spotify
// application there is no fleet credential to check either: every broadcaster
// registers their own app and connects their own account. What remains is the
// modules-side resolver that hands both over per call; without it the provider
// can authenticate nothing, so it is skipped.
func appendSpotify(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.SpotifyKeys == nil,
		"spotify provider disabled: no credential resolver (modules spotify RPC unwired)",
		spotify.New, spotify.Config{
			BaseURL:     cfg.SpotifyBaseURL,
			AccountsURL: cfg.SpotifyAccountsURL,
			RateLimit:   cfg.SpotifyRateLimit,
		}, d)
}

// appendCustom adds the urlfetch provider behind its definition source: with
// no projected definitions to execute there is nothing to serve, the same
// degrade as a credential-less provider. (The FetchDefs adapter lands with
// the commands-service projection; main.wireKeyResolvers records the seam.)
func appendCustom(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.FetchDefs == nil, "custom urlfetch provider disabled: no definition source (projection FetchDefs unwired)", custom.New, custom.Config{
		ChannelRateLimit: cfg.CustomChannelRateLimit,
		DefRateLimit:     cfg.CustomDefRateLimit,
		HostRateLimit:    cfg.CustomHostRateLimit,
		PositiveTTL:      cfg.CustomPositiveTTL,
	}, d)
}
