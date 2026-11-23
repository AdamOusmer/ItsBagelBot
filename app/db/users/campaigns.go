// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"slices"
	"strings"

	"go.uber.org/zap"

	"ItsBagelBot/app/db/users/campaign"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/pkg/email"
	"ItsBagelBot/pkg/env"
)

func newCampaignService(repo *repository.Users, log *zap.Logger) *campaign.Service {
	cfg, err := email.FromEnv("RESEND_MARKETING")
	if err != nil {
		log.Info("marketing campaigns disabled: RESEND_MARKETING_API not configured")
		return nil
	}
	settings := campaign.Settings{
		From:           strings.TrimSpace(cfg.From),
		Origin:         strings.TrimSpace(env.Get("DASHBOARD_ORIGIN", "")),
		MailingAddress: strings.TrimSpace(env.Get("BUSINESS_MAILING_ADDRESS", "")),
		SupportEmail:   env.Get("MARKETING_SUPPORT_EMAIL", ""),
	}
	if slices.Contains([]string{settings.From, settings.Origin, settings.MailingAddress}, "") {
		log.Warn("marketing campaigns disabled: RESEND_MARKETING_FROM, DASHBOARD_ORIGIN and BUSINESS_MAILING_ADDRESS are required")
		return nil
	}
	return campaign.New(repo, email.New(cfg), settings, log)
}
