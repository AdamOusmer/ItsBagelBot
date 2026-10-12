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

func SubscribeConnection(w Wiring, invalidationPrefix string) error {
	handler := func(ctx context.Context, req notificationsrpc.ConnectionRequest, retract bool) notificationsrpc.ConnectionReply {
		if req.AccountID == 0 || req.AccountCreatedAt <= 0 || req.IncidentID == "" || req.DeliveryDeadline.IsZero() || req.ExpiresAt.IsZero() {
			return notificationsrpc.ConnectionReply{Refusal: bus.Classify(fmt.Errorf("connection identity and deadlines required"))}
		}
		p := repository.ConnectionParams{AccountID: req.AccountID, Incarnation: strconv.FormatInt(req.AccountCreatedAt, 10), IncidentID: req.IncidentID, DeliveryDeadline: req.DeliveryDeadline, ExpiresAt: req.ExpiresAt, Title: req.Title, Body: req.Body, Level: notification.Level(req.Level)}
		if p.Level == "" {
			p.Level = notification.LevelWarning
		}
		if !retract {
			if err := notification.LevelValidator(p.Level); err != nil {
				return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
			}
			if p.Title == "" || p.Body == "" {
				return notificationsrpc.ConnectionReply{Refusal: bus.Classify(fmt.Errorf("title and body required"))}
			}
			// Existing receipts remain authoritative even after Users records acknowledgment.
			if existing, ok, err := w.Repo.ConnectionOutcome(ctx, p); err != nil {
				return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
			} else if ok {
				return notificationsrpc.ConnectionReply{Outcome: existing.Outcome, NotificationID: existing.NotificationID}
			}
			eligible, err := bus.RequestJSONTimeout[usersrpc.LifecycleReply](ctx, w.NC, usersrpc.LifecycleSubject, usersrpc.LifecycleRequest{Action: "notice_eligible", UserID: req.AccountID, AccountCreatedAt: req.AccountCreatedAt, IncidentID: req.IncidentID}, readBudget)
			if err != nil {
				return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
			}
			if !eligible.Applied {
				retract = true
			}
		}
		var result repository.ConnectionResult
		var err error
		if retract {
			result, err = w.Repo.RetractConnection(ctx, p, time.Now())
		} else {
			result, err = w.Repo.CreateConnection(ctx, p, time.Now())
		}
		if err != nil {
			return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
		}
		if result.Created || retract {
			_ = invalidate.Publish(w.NC, invalidationPrefix, "notifications", strconv.FormatUint(req.AccountID, 10))
		}
		return notificationsrpc.ConnectionReply{Outcome: result.Outcome, NotificationID: result.NotificationID}
	}
	if err := bus.Serve(w.Within(sendBudget), notificationsrpc.ConnectionSubject+".send", func(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
		return handler(ctx, req, false)
	}); err != nil {
		return err
	}
	if err := bus.Serve(w.Within(sendBudget), notificationsrpc.ConnectionSubject+".erase", func(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
		err := w.Repo.EraseConnection(ctx, req.AccountID, strconv.FormatInt(req.AccountCreatedAt, 10), req.ErasedAt)
		if err != nil {
			return notificationsrpc.ConnectionReply{Refusal: bus.Classify(err)}
		}
		_ = invalidate.Publish(w.NC, invalidationPrefix, "notifications", strconv.FormatUint(req.AccountID, 10))
		return notificationsrpc.ConnectionReply{Outcome: notificationsrpc.OutcomeErased}
	}); err != nil {
		return err
	}
	return bus.Serve(w.Within(sendBudget), notificationsrpc.ConnectionSubject+".retract", func(ctx context.Context, req notificationsrpc.ConnectionRequest) notificationsrpc.ConnectionReply {
		return handler(ctx, req, true)
	})
}
