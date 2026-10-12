package notificationsrpc

import (
	"ItsBagelBot/internal/domain/rpc"
	"time"
)

const OutcomeErased = "erased"

const ConnectionSubject = "bagel.rpc.internal.notifications.connection"

type ConnectionRequest struct {
	AccountID        uint64    `json:"account_id"`
	AccountCreatedAt int64     `json:"account_created_at"`
	IncidentID       string    `json:"incident_id"`
	DeliveryDeadline time.Time `json:"delivery_deadline"`
	ErasedAt         time.Time `json:"erased_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	Level            string    `json:"level"`
}
type ConnectionReply struct {
	Outcome        string `json:"outcome"`
	NotificationID int    `json:"notification_id,omitempty"`
	rpc.Refusal
}
