// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/ent/giveawayaward"
	giveawayengine "ItsBagelBot/app/db/transactions/giveaway"
	"ItsBagelBot/app/db/transactions/mail"
	transactionsrpc "ItsBagelBot/app/db/transactions/rpc"
	"ItsBagelBot/app/db/transactions/tebex"
	transactionsweb "ItsBagelBot/app/db/transactions/web"
	"ItsBagelBot/pkg/env"
	"go.uber.org/zap"
)

type giveawayRuntimeConfig struct {
	Store    *giveawayengine.Store
	Users    *transactionsrpc.UsersGiveawayClient
	Mailer   *mail.Mailer
	Provider tebex.RecurringProvider
	Config   giveawayengine.Config
}

type billingAlertInput struct {
	award   *ent.GiveawayAward
	eventID string
	message string
	alertID string
}

func recordGiveawayBillingIncident(ctx context.Context, db *ent.Client, incident transactionsweb.BillingIncident) error {
	if incident.UserID == 0 {
		return nil
	}
	awards, err := db.GiveawayAward.Query().Where(giveawayaward.UserIDEQ(incident.UserID), giveawayaward.StateIn("selected", "preparing", "needs_review", "scheduled", "active", "completed")).All(ctx)
	if err != nil {
		return err
	}
	for _, award := range awards {
		if !billingIncidentAffectsAward(award, incident.OccurredAt) {
			continue
		}
		message := "provider event " + incident.EventType + " applied while giveaway award was " + award.State
		if err := upsertBillingAlert(ctx, db, billingAlertInput{award: award, eventID: incident.EventID, message: message, alertID: award.ID + ":billing:" + incident.EventID}); err != nil {
			return err
		}
	}
	return nil
}

func upsertBillingAlert(ctx context.Context, db *ent.Client, input billingAlertInput) error {
	alert, err := db.GiveawayAlert.Query().Where(giveawayalert.IDEQ(input.alertID)).Only(ctx)
	if ent.IsNotFound(err) {
		return createBillingAlert(ctx, db, input)
	}
	if err != nil {
		return err
	}
	if alert.State == "resolved" {
		return nil
	}
	_, err = alert.Update().SetOperationID(input.eventID).SetState("unresolved").SetMessage(input.message).SetLastSeenAt(time.Now().UTC()).Save(ctx)
	return err
}

func createBillingAlert(ctx context.Context, db *ent.Client, input billingAlertInput) error {
	builder := db.GiveawayAlert.Create().SetID(input.alertID).SetAwardID(input.award.ID).SetOperationID(input.eventID).SetCategory("billing-event:" + input.eventID).SetState("unresolved").SetMessage(input.message)
	if !input.award.PlannedStart.IsZero() {
		builder.SetAffectedBoundary(input.award.PlannedStart)
	}
	_, err := builder.Save(ctx)
	return err
}

func billingIncidentAffectsAward(award *ent.GiveawayAward, occurredAt time.Time) bool {
	if award.State != "completed" {
		return true
	}
	return !occurredAt.IsZero() && !occurredAt.Before(award.ConfirmedStart) && occurredAt.Before(award.ConfirmedEnd)
}

func newGiveawayEngine(cfg giveawayRuntimeConfig) *giveawayengine.Engine {
	engineConfig := giveawayengine.EngineConfig{Store: cfg.Store, Users: cfg.Users, Provider: cfg.Provider, Config: cfg.Config}
	if cfg.Mailer != nil {
		engineConfig.Mailer = cfg.Mailer
	}
	return giveawayengine.NewEngine(engineConfig)
}

func newGiveawayProvider(config giveawayengine.Config, log *zap.Logger) (tebex.RecurringProvider, bool) {
	privateKey := env.Get("TEBEX_CHECKOUT_PRIVATE_KEY", "")
	projectID := env.Get("TEBEX_CHECKOUT_PROJECT_ID", "")
	if privateKey == "" || projectID == "" {
		config.ProviderMutations = false
		return nil, false
	}
	client, err := tebex.NewCheckoutClient(tebex.CheckoutConfig{ProjectID: projectID, PrivateKey: privateKey, EnableMutations: config.CanMutateProvider(), MinimumNotice: config.RenewalBuffer})
	if err != nil {
		log.Warn("giveaway provider disabled", zap.Error(err))
		return nil, false
	}
	return client, true
}
