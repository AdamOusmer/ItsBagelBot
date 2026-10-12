package rpc

import (
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/domain/invalidate"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"fmt"
	"strconv"
	"time"
)

type connectionRPC struct {
	Wiring
	invalidationPrefix string
}

func SubscribeConnection(w Wiring, invalidationPrefix string) error {
	handler := &connectionRPC{Wiring: w, invalidationPrefix: invalidationPrefix}
	return bus.ServeVerbs(w.Within(sendBudget), notificationsrpc.ConnectionSubject,
		bus.At("send", handler.send), bus.At("retract", handler.retract), bus.At("erase", handler.erase))
}
func connectionParams(req notificationsrpc.ConnectionRequest) (repository.ConnectionParams, error) {
	p := repository.ConnectionParams{AccountID: req.AccountID, Incarnation: strconv.FormatInt(req.AccountCreatedAt, 10), IncidentID: req.IncidentID, DeliveryDeadline: req.DeliveryDeadline, ExpiresAt: req.ExpiresAt, Title: req.Title, Body: req.Body, Level: notification.Level(req.Level)}
	if req.AccountID == 0 {
		return p, fmt.Errorf("account identity required")
	}
	if req.AccountCreatedAt <= 0 {
		return p, fmt.Errorf("account incarnation required")
	}
	if req.IncidentID == "" {
		return p, fmt.Errorf("incident identity required")
	}
	if req.DeliveryDeadline.IsZero() || req.ExpiresAt.IsZero() {
		return p, fmt.Errorf("connection deadlines required")
	}
	if p.Level == "" {
		p.Level = notification.LevelWarning
	}
	return p, nil
}
func connectionSendParams(req notificationsrpc.ConnectionRequest) (repository.ConnectionParams, error) {
	p, err := connectionParams(req)
	if err != nil {
		return p, err
	}
	if err = notification.LevelValidator(p.Level); err != nil {
		return p, err
	}
	if p.Title == "" || p.Body == "" {
		return p, fmt.Errorf("title and body required")
	}
	return p, nil
}
func (c *connectionRPC) send(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
	p, err := connectionSendParams(req)
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	// Existing receipts remain authoritative after Users records acknowledgment.
	existing, found, err := c.Repo.ConnectionOutcome(ctx, p)
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	if found {
		return notificationsrpc.ConnectionReply{Outcome: existing.Outcome, NotificationID: existing.NotificationID}
	}
	eligible, err := bus.RequestJSONTimeout[usersrpc.LifecycleReply](ctx, c.NC, usersrpc.LifecycleSubject, usersrpc.LifecycleRequest{Action: "notice_eligible", UserID: req.AccountID, AccountCreatedAt: req.AccountCreatedAt, IncidentID: req.IncidentID}, readBudget)
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	if !eligible.Applied {
		return c.retract(ctx, req)
	}
	result, err := c.Repo.CreateConnection(ctx, p, time.Now())
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	if result.Created {
		c.invalidate(req.AccountID)
	}
	return notificationsrpc.ConnectionReply{Outcome: result.Outcome, NotificationID: result.NotificationID}
}
func (c *connectionRPC) retract(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
	p, err := connectionParams(req)
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	result, err := c.Repo.RetractConnection(ctx, p, time.Now())
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	c.invalidate(req.AccountID)
	return notificationsrpc.ConnectionReply{Outcome: result.Outcome, NotificationID: result.NotificationID}
}
func (c *connectionRPC) erase(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
	err := c.Repo.EraseConnection(ctx, req.AccountID, strconv.FormatInt(req.AccountCreatedAt, 10), req.ErasedAt)
	if err != nil {
		return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
	}
	c.invalidate(req.AccountID)
	return notificationsrpc.ConnectionReply{Outcome: notificationsrpc.OutcomeErased}
}
func (c *connectionRPC) invalidate(accountID uint64) {
	_ = invalidate.Publish(c.NC, c.invalidationPrefix, "notifications", strconv.FormatUint(accountID, 10))
}
