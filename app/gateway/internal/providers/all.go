// Package providers wires every external API system the gateway serves, the
// twin of sesame's app/sesame/modules package: each system lives in its own
// subpackage, and adding one is writing that package plus one entry here.
package providers

import (
	"ItsBagelBot/app/gateway/internal/config"
	"ItsBagelBot/app/gateway/internal/provider"
	"ItsBagelBot/app/gateway/internal/providers/hypixel"
	"ItsBagelBot/app/gateway/internal/providers/mcsr"
	"ItsBagelBot/app/gateway/internal/providers/urchin"

	"go.uber.org/zap"
)

// All builds every configured provider, in registration order. A provider
// missing its credentials is skipped with a warning: its subjects then simply
// time out at the caller, the same failure mode as the upstream being down.
func All(cfg *config.Config, d provider.Deps) []provider.Provider {
	log := d.Log
	if log == nil {
		log = zap.NewNop()
	}

	var out []provider.Provider

	if cfg.UrchinAPIKey != "" {
		out = append(out, urchin.New(urchin.Config{
			BaseURL:   cfg.UrchinBaseURL,
			APIKey:    cfg.UrchinAPIKey,
			RateLimit: cfg.UrchinRateLimit,
		}, d))
	} else {
		log.Warn("urchin provider disabled: URCHIN_API_KEY not set")
	}

	if cfg.HypixelAPIKey != "" {
		out = append(out, hypixel.New(hypixel.Config{
			BaseURL:       cfg.HypixelBaseURL,
			MojangBaseURL: cfg.MojangBaseURL,
			APIKey:        cfg.HypixelAPIKey,
			RateLimit:     cfg.HypixelRateLimit,
		}, d))
	} else {
		log.Warn("hypixel provider disabled: HYPIXEL_API_KEY not set (!bwstats will not answer)")
	}

	if cfg.McsrEnabled {
		out = append(out, mcsr.New(mcsr.Config{
			BaseURL:   cfg.McsrBaseURL,
			APIKey:    cfg.McsrAPIKey,
			RateLimit: cfg.McsrRateLimit,
		}, d))
	} else {
		log.Warn("mcsr provider disabled: MCSR_ENABLED=false")
	}

	return out
}
