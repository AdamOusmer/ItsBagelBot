// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package providers wires every external API system gossip serves, the
// twin of sesame's app/sesame/modules package: each system lives in its own
// subpackage, and adding one is writing that package plus one entry here.
package providers

import (
	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/fortnite"
	"ItsBagelBot/app/gossip/internal/providers/govee"
	"ItsBagelBot/app/gossip/internal/providers/hypixel"
	"ItsBagelBot/app/gossip/internal/providers/mcsr"
	"ItsBagelBot/app/gossip/internal/providers/paceman"
	"ItsBagelBot/app/gossip/internal/providers/urchin"

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
	return out
}

// appendIf is the shape every appendX helper below shares: gate on a
// condition, log why and leave out unchanged when it skips, otherwise build
// the provider and append it. Factoring this once means adding a provider
// with the same "one credential/flag gates it" shape is writing a gate
// condition, a skip reason and a constructor closure — not a fifth copy of
// this whole sequence (the duplication CodeScene flagged once appendPaceman
// made the third near-identical copy).
func appendIf(out []provider.Provider, log *zap.Logger, skip bool, skipReason string, build func() provider.Provider) []provider.Provider {
	if skip {
		log.Warn(skipReason)
		return out
	}
	return append(out, build())
}

func appendUrchin(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return appendIf(out, log, cfg.UrchinAPIKey == "", "urchin provider disabled: URCHIN_API_KEY not set", func() provider.Provider {
		return urchin.New(urchin.Config{
			BaseURL:   cfg.UrchinBaseURL,
			APIKey:    cfg.UrchinAPIKey,
			RateLimit: cfg.UrchinRateLimit,
		}, d)
	})
}

func appendHypixel(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return appendIf(out, log, cfg.HypixelAPIKey == "", "hypixel provider disabled: HYPIXEL_API_KEY not set (!bwstats will not answer)", func() provider.Provider {
		return hypixel.New(hypixel.Config{
			BaseURL:         cfg.HypixelBaseURL,
			MojangBaseURL:   cfg.MojangBaseURL,
			APIKey:          cfg.HypixelAPIKey,
			RateLimit:       cfg.HypixelRateLimit,
			MojangRateLimit: cfg.MojangRateLimit,
		}, d)
	})
}

func appendMcsr(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return appendIf(out, log, !cfg.McsrEnabled, "mcsr provider disabled: MCSR_ENABLED=false", func() provider.Provider {
		return mcsr.New(mcsr.Config{
			BaseURL:   cfg.McsrBaseURL,
			APIKey:    cfg.McsrAPIKey,
			RateLimit: cfg.McsrRateLimit,
		}, d)
	})
}

// appendPaceman adds the paceman provider. Its public API needs no key, so
// unlike appendUrchin/appendHypixel there is no credential to gate on — the
// only switch is the operator-controlled PacemanEnabled kill switch.
func appendPaceman(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	return appendIf(out, log, !cfg.PacemanEnabled, "paceman provider disabled: PACEMAN_ENABLED=false", func() provider.Provider {
		return paceman.New(paceman.Config{
			BaseURL:     cfg.PacemanBaseURL,
			UserBaseURL: cfg.PacemanUserBaseURL,
			RateLimit:   cfg.PacemanRateLimit,
		}, d)
	})
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
	return appendIf(out, log, d.GoveeKeys == nil, "govee provider disabled: no key resolver (modules govee RPC unwired)", func() provider.Provider {
		return govee.New(govee.Config{
			BaseURL:   cfg.GoveeBaseURL,
			RateLimit: cfg.GoveeRateLimit,
		}, d)
	})
}
