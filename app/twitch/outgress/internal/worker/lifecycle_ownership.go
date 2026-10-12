package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"strconv"
)

func (w *Worker) recordChatOwnership(ctx context.Context, e enrollment) {
	if w.lifecycleNC == nil {
		return
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return
	}
	current, err := w.lifecycleRequest(ctx, usersrpc.LifecycleRequest{UserID: uid, Action: "get"})
	if err != nil {
		return
	}
	if !ownershipEnrollmentMatches(e, current) {
		return
	}
	e.incarnation = current.AccountCreatedAt
	e.generation = current.State.AdmissionGeneration
	inventory := subscriptionOwnership{ids: []string{}}
	err = w.visitSubscriptionPages(ctx, e, func(sub twitch.EventSubEntry) error {
		if !ownedSub(sub, e) {
			return nil
		}
		if w.botOwnedSubscription(sub) {
			inventory.add(sub)
		}
		return nil
	})
	if err != nil {
		return
	}
	_ = w.registry.SetIncarnation(ctx, e.broadcasterID, current.AccountCreatedAt)
	_, _ = w.mutateLifecycle(ctx, inventory.request(uid, current))
}

func ownershipEnrollmentMatches(e enrollment, current usersrpc.LifecycleReply) bool {
	if e.incarnation == 0 {
		return true
	}
	if current.AccountCreatedAt != e.incarnation {
		return false
	}
	return current.State.AdmissionGeneration == e.generation
}

type subscriptionOwnership struct {
	ids        []string
	chatID     string
	ban, unban bool
}

func (o *subscriptionOwnership) add(sub twitch.EventSubEntry) {
	o.ids = append(o.ids, sub.ID)
	switch sub.Type {
	case "channel.chat.message":
		o.chatID = sub.ID
	case "channel.ban":
		o.ban = sub.Status == "enabled"
	case "channel.unban":
		o.unban = sub.Status == "enabled"
	}
}
func (o subscriptionOwnership) request(uid uint64, current usersrpc.LifecycleReply) usersrpc.LifecycleRequest {
	healthy := o.ban && o.unban
	return usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: "subscription_owned", SubscriptionID: o.chatID, SubscriptionIDs: o.ids, ControlListenersHealthy: &healthy}
}
