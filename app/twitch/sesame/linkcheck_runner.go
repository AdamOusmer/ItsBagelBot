// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/automod/linkcheck"
	"ItsBagelBot/app/twitch/sesame/internal/config"
	"ItsBagelBot/app/twitch/sesame/module"

	"go.uber.org/zap"
)

const linkCheckFeedInterval = 30 * time.Minute

func runLinkCheck(ctx context.Context, guard *automod.Gate, cfg *config.Config, log *zap.Logger) {
	checker := linkcheck.NewChecker(linkcheck.Options{
		ExpandShorteners: cfg.LinkCheckShorteners,
		Feeds:            linkcheck.NewFeeds(linkcheck.DefaultFeedSources, nil),
		Log:              log,
	})
	checker.OnBad = func(h linkcheck.Hit) {
		log.Warn("linkcheck conviction",
			zap.String("host", h.Host),
			zap.String("token", h.Token),
			zap.String("via", h.Via),
			zap.String("source", string(h.Source)),
			module.BIDField(h.Channel),
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
