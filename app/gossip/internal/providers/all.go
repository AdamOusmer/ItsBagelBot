// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package providers

import (
	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/clashroyale"
	"ItsBagelBot/app/gossip/internal/providers/codm"
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
	out = appendCODM(out, cfg, d, log)
	out = appendGovee(out, cfg, d, log)
	out = appendClashRoyale(out, cfg, d, log)
	out = appendValorant(out, cfg, d, log)
	out = appendSpotify(out, cfg, d, log)
	out = appendCustom(out, cfg, d, log)
	return out
}

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
	if cfg.HypixelAPIKey == "" {
		log.Warn("hypixel provider running uuid-only: HYPIXEL_API_KEY not set (!bwstats will not answer)")
	}
	return append(out, hypixel.New(hypixel.Config{
		BaseURL:         cfg.HypixelBaseURL,
		MojangBaseURL:   cfg.MojangBaseURL,
		APIKey:          cfg.HypixelAPIKey,
		RateLimit:       cfg.HypixelRateLimit,
		MojangRateLimit: cfg.MojangRateLimit,
	}, d))
}

func appendMcsr(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, !cfg.McsrEnabled, "mcsr provider disabled: MCSR_ENABLED=false", mcsr.New, mcsr.Config{
		BaseURL:   cfg.McsrBaseURL,
		APIKey:    cfg.McsrAPIKey,
		RateLimit: cfg.McsrRateLimit,
	}, d)
}

func appendPaceman(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, !cfg.PacemanEnabled, "paceman provider disabled: PACEMAN_ENABLED=false", paceman.New, paceman.Config{
		BaseURL:     cfg.PacemanBaseURL,
		UserBaseURL: cfg.PacemanUserBaseURL,
		RateLimit:   cfg.PacemanRateLimit,
	}, d)
}

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

func appendCODM(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, !cfg.CODMEnabled, "codm provider disabled: CODM_ENABLED=false", codm.New, codm.Config{
		BaseURL:   cfg.CODMBaseURL,
		Country:   cfg.CODMCountry,
		RateLimit: cfg.CODMRateLimit,
	}, d)
}

func appendGovee(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.GoveeKeys == nil, "govee provider disabled: no key resolver (modules govee RPC unwired)", govee.New, govee.Config{
		BaseURL:   cfg.GoveeBaseURL,
		RateLimit: cfg.GoveeRateLimit,
	}, d)
}

func appendClashRoyale(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.ClashRoyaleAPIKey == "", "clashroyale provider disabled: CLASHROYALE_API_KEY not set (!cr commands will not answer)", clashroyale.New, clashroyale.Config{
		BaseURL:   cfg.ClashRoyaleBaseURL,
		APIKey:    cfg.ClashRoyaleAPIKey,
		RateLimit: cfg.ClashRoyaleRateLimit,
	}, d)
}

func appendValorant(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, cfg.ValorantAPIKey == "", "valorant provider disabled: VALORANT_API_KEY not set (!val commands will not answer)", valorant.New, valorant.Config{
		BaseURL:          cfg.ValorantBaseURL,
		ContentBaseURL:   cfg.ValorantContentBaseURL,
		APIKey:           cfg.ValorantAPIKey,
		RateLimit:        cfg.ValorantRateLimit,
		ContentRateLimit: cfg.ValorantContentRateLimit,
	}, d)
}

func appendSpotify(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.SpotifyKeys == nil,
		"spotify provider disabled: no credential resolver (modules spotify RPC unwired)",
		spotify.New, spotify.Config{
			BaseURL:     cfg.SpotifyBaseURL,
			AccountsURL: cfg.SpotifyAccountsURL,
			RateLimit:   cfg.SpotifyRateLimit,
		}, d)
}

func appendCustom(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return gated(out, log, d.FetchDefs == nil, "custom urlfetch provider disabled: no definition source (projection FetchDefs unwired)", custom.New, custom.Config{
		ChannelRateLimit: cfg.CustomChannelRateLimit,
		DefRateLimit:     cfg.CustomDefRateLimit,
		HostRateLimit:    cfg.CustomHostRateLimit,
		PositiveTTL:      cfg.CustomPositiveTTL,
	}, d)
}
