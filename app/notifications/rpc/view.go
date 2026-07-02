package rpc

import (
	"ItsBagelBot/app/notifications/ent"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
)

func viewOf(row *ent.Notification, read bool) notificationsrpc.NotificationView {
	return notificationsrpc.NotificationView{
		ID:             int64(row.ID),
		Scope:          string(row.Scope),
		Title:          row.Title,
		Body:           row.Body,
		Level:          string(row.Level),
		TargetUserID:   row.TargetUserID,
		CreatedByLogin: row.CreatedByLogin,
		CreatedAt:      row.CreatedAt,
		ExpiresAt:      row.ExpiresAt,
		Read:           read,
	}
}
