// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package giveaway

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/repository"
)

type awardAlert struct {
	awardID, category, operationID, message string
	now                                     time.Time
	reopen                                  bool
	retryOnRace                             bool
}

func upsertAwardAlert(ctx context.Context, db *ent.Client, alert awardAlert) error {
	return repository.UpsertAlert(ctx, db, repository.AlertUpsert{
		Match:          giveawayalert.And(giveawayalert.AwardIDEQ(alert.awardID), giveawayalert.CategoryEQ(alert.category)),
		Create:         alert.create,
		Update:         alert.update,
		ReopenResolved: true,
		RetryOnRace:    alert.retryOnRace,
	})
}

func (alert awardAlert) create(builder *ent.GiveawayAlertCreate) {
	builder.SetID(alert.awardID + ":" + alert.category).SetAwardID(alert.awardID).SetCategory(alert.category).SetState("unresolved").SetMessage(alert.message).SetLastSeenAt(alert.now)
	if alert.operationID != "" {
		builder.SetOperationID(alert.operationID)
	}
}

func (alert awardAlert) update(builder *ent.GiveawayAlertUpdateOne) {
	builder.SetState("unresolved").SetMessage(alert.message).SetLastSeenAt(alert.now)
	if alert.reopen {
		builder.ClearAcknowledgedBy().ClearAcknowledgedAt().ClearResolvedAt()
	}
}
