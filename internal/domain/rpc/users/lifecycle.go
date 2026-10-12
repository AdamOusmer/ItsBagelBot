package usersrpc

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/rpc"
	"time"
)

const LifecycleSubject = "bagel.rpc.internal.users.lifecycle"

type LifecycleRequest struct {
	SubscriptionIDs         []string  `json:"subscription_ids,omitempty"`
	ControlListenersHealthy *bool     `json:"control_listeners_healthy,omitempty"`
	SubscriptionID          string    `json:"subscription_id,omitempty"`
	UserID                  uint64    `json:"user_id"`
	AccountCreatedAt        int64     `json:"account_created_at"`
	AdmissionGeneration     int64     `json:"admission_generation"`
	Action                  string    `json:"action"`
	Reason                  string    `json:"reason,omitempty"`
	EventID                 string    `json:"event_id,omitempty"`
	ObservedAt              time.Time `json:"observed_at,omitempty"`
	TimeoutUntil            time.Time `json:"timeout_until,omitempty"`
	OperationID             string    `json:"operation_id,omitempty"`
	IncidentID              string    `json:"incident_id,omitempty"`
}
type LifecycleWork struct {
	UserID           uint64          `json:"user_id"`
	AccountCreatedAt int64           `json:"account_created_at"`
	State            lifecycle.State `json:"state"`
}
type LifecycleErasure struct {
	DeletedAt         time.Time       `json:"deleted_at"`
	UserID            uint64          `json:"user_id"`
	AccountCreatedAt  int64           `json:"account_created_at"`
	State             lifecycle.State `json:"state"`
	NotificationsDone bool            `json:"notifications_done"`
	OutgressDone      bool            `json:"outgress_done"`
}
type LifecycleReply struct {
	ControlRepair    []LifecycleWork    `json:"control_repair,omitempty"`
	Cleanup          []LifecycleWork    `json:"cleanup,omitempty"`
	Dormancy         []LifecycleWork    `json:"dormancy,omitempty"`
	Seeded           []LifecycleWork    `json:"seeded,omitempty"`
	Erasures         []LifecycleErasure `json:"erasures,omitempty"`
	Items            []LifecycleWork    `json:"items,omitempty"`
	State            lifecycle.State    `json:"state"`
	AccountCreatedAt int64              `json:"account_created_at"`
	StateRevision    int64              `json:"state_revision"`
	Applied          bool               `json:"applied"`
	rpc.Refusal
}
