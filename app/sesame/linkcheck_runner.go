// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/sesame/automod"
	"ItsBagelBot/app/sesame/automod/linkcheck"
	"ItsBagelBot/app/sesame/internal/config"

	"go.uber.org/zap"
)

// linkCheckFeedInterval is how often the community blocklists re-pull. The
// feeds update on the order of minutes and phishing infra churns in hours;
// thirty minutes bounds staleness at a rounding error of outbound traffic
// (two small GETs per tick).
const linkCheckFeedInterval = 30 * time.Minute

// runLinkCheck arms the gate's dynamic link-safety layer and keeps its
// blocklist snapshot current. It follows the refresher contract: install once
// at startup, re-pull on a slow ticker, never let a failed fetch blank live
// coverage (the previous snapshot serves until one succeeds). The checker's
// workers ride ctx, so cancelling the service context drains them; nothing to
// Close.
//
// Convictions land in OnBad as a WARN audit line rather than an action of
// their own - enforcement flows through the normal verdict path (the phish
// rule fires on the next line carrying that host), so this hook stays the
// shadow-mode paper trail: who posted what where, per source.
func runLinkCheck(ctx context.Context, guard *automod.Gate, cfg *config.Config, log *zap.Logger) {
	sources := linkcheck.DefaultFeedSources
	if cfg.URLHausAuthKey != "" {
		sources = append(sources, linkcheck.FeedSource{
			Name:    "urlhaus",
			URL:     "https://urlhaus.abuse.ch/downloads/hostfile/",
			AuthKey: cfg.URLHausAuthKey,
			Format:  linkcheck.FormatHosts,
		})
	}

	checker := linkcheck.NewChecker(linkcheck.Options{
		ExpandShorteners: cfg.LinkCheckShorteners,
		Feeds:            linkcheck.NewFeeds(sources, nil),
		Log:              log,
	})
	checker.OnBad = func(h linkcheck.Hit) {
		log.Warn("linkcheck conviction",
			zap.String("host", h.Host),
			zap.String("token", h.Token),
			zap.String("via", h.Via),
			zap.String("source", string(h.Source)),
			zap.Uint64("broadcaster_id", h.Channel),
			zap.String("chatter_id", h.Sender))
	}
	guard.SetLinkChecker(checker)
	checker.Start(ctx)
	log.Info("linkcheck armed",
		zap.Bool("feeds", cfg.LinkCheckFeeds),
		zap.Bool("shorteners", cfg.LinkCheckShorteners))

	if !cfg.LinkCheckFeeds {
		return
	}
	refresh := func() {
		if _, err := checker.RefreshFeeds(ctx); err != nil {
			log.Warn("linkcheck feed refresh failed; keeping previous set", zap.Error(err))
		}
	}
	refresh()
	ticker := time.NewTicker(linkCheckFeedInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refresh()
		}
	}
}
