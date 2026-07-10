// Package providers wires every external API system the gateway serves, the
// twin of sesame's app/sesame/modules package: each system lives in its own
// subpackage, and adding one is writing that package plus one entry here.
package providers

import (
	"ItsBagelBot/app/gateway/internal/config"
	"ItsBagelBot/app/gateway/internal/provider"
	"ItsBagelBot/app/gateway/internal/providers/fortnite"
	"ItsBagelBot/app/gateway/internal/providers/govee"
	"ItsBagelBot/app/gateway/internal/providers/hypixel"
	"ItsBagelBot/app/gateway/internal/providers/mcsr"
	"ItsBagelBot/app/gateway/internal/providers/urchin"

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
	out = appendFortnite(out, cfg, d, log)
	out = appendGovee(out, cfg, d, log)
	return out
}

func appendUrchin(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if cfg.UrchinAPIKey == "" {
		log.Warn("urchin provider disabled: URCHIN_API_KEY not set")
		return out
	}
	return append(out, urchin.New(urchin.Config{
		BaseURL:   cfg.UrchinBaseURL,
		APIKey:    cfg.UrchinAPIKey,
		RateLimit: cfg.UrchinRateLimit,
	}, d))
}

func appendHypixel(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if cfg.HypixelAPIKey == "" {
		log.Warn("hypixel provider disabled: HYPIXEL_API_KEY not set (!bwstats will not answer)")
		return out
	}
	return append(out, hypixel.New(hypixel.Config{
		BaseURL:       cfg.HypixelBaseURL,
		MojangBaseURL: cfg.MojangBaseURL,
		APIKey:        cfg.HypixelAPIKey,
		RateLimit:     cfg.HypixelRateLimit,
	}, d))
}

func appendMcsr(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if !cfg.McsrEnabled {
		log.Warn("mcsr provider disabled: MCSR_ENABLED=false")
		return out
	}
	return append(out, mcsr.New(mcsr.Config{
		BaseURL:   cfg.McsrBaseURL,
		APIKey:    cfg.McsrAPIKey,
		RateLimit: cfg.McsrRateLimit,
	}, d))
}

// appendFortnite adds the fortnite provider. It is double-gated: the
// FORTNITE_ENABLED flag keeps it dark until tested against a real key, and the
// key itself is required for the stats endpoint's Authorization header.
func appendFortnite(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if !cfg.FortniteEnabled {
		log.Warn("fortnite provider disabled: FORTNITE_ENABLED=false")
		return out
	}
	if cfg.FortniteAPIKey == "" {
		log.Warn("fortnite provider disabled: FORTNITE_API_KEY not set (!fnstats and !store will not answer)")
		return out
	}
	return append(out, fortnite.New(fortnite.Config{
		BaseURL:   cfg.FortniteBaseURL,
		APIKey:    cfg.FortniteAPIKey,
		RateLimit: cfg.FortniteRateLimit,
	}, d))
}

// appendGovee adds the govee provider. It needs no service key — each
// broadcaster brings their own — but it does need the key resolver to fetch
// them; without it (the modules internal key RPC unwired) there is nothing to
// authenticate with, so it is skipped like any credential-less provider.
func appendGovee(out []provider.Provider, cfg *config.Config, d provider.Deps, log *zap.Logger) []provider.Provider {
	if d.GoveeKeys == nil {
		log.Warn("govee provider disabled: no key resolver (modules govee RPC unwired)")
		return out
	}
	return append(out, govee.New(govee.Config{
		BaseURL:   cfg.GoveeBaseURL,
		RateLimit: cfg.GoveeRateLimit,
	}, d))
}
