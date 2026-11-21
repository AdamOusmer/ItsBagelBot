// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminauditdelivery"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

func (r *Users) tryDeliverAdminAudit(ctx context.Context, delivery *ent.AdminAuditDelivery) {
	if err := r.deliverAdminAudit(ctx, delivery); err != nil {
		r.log.Warn("admin audit delivery pending", zap.String("operation_id", delivery.ID.String()), zap.Error(err))
	}
}

func (r *Users) deliverAdminAudit(ctx context.Context, delivery *ent.AdminAuditDelivery) error {
	req := delivery.Payload
	req.AuditID = delivery.ID.String()
	if err := r.adminAuth.AppendAuditDelivery(ctx, req); err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.AdminAuditDelivery.Delete().Where(adminauditdelivery.IDEQ(delivery.ID)).Exec(ctx)
		return err
	})
}

func (r *Users) DeliverAdminAudits(ctx context.Context, _ time.Time) (int, error) {
	deliveries, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.AdminAuditDelivery, error) {
		return r.client.AdminAuditDelivery.Query().Order(ent.Asc(adminauditdelivery.FieldCreatedAt), ent.Asc(adminauditdelivery.FieldID)).All(ctx)
	})
	if err != nil {
		return 0, err
	}
	for i, delivery := range deliveries {
		if err := r.deliverAdminAudit(ctx, delivery); err != nil {
			return i, err
		}
	}
	return len(deliveries), nil
}
