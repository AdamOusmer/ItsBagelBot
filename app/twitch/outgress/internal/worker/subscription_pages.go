package worker

import (
	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"context"
)

// Provider pagination is bounded; an incomplete inventory never becomes durable ownership.
func (w *Worker) visitSubscriptionPages(ctx context.Context, e enrollment, visit func(twitch.EventSubEntry) error) error {
	cursor := ""
	for page := 0; page < 10; page++ {
		if !w.enrollmentIdentityMatches(ctx, e) {
			return errLifecycleStopped
		}
		next, err := w.visitSubscriptionPage(ctx, e, cursor, visit)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		cursor = next
	}
	return errLifecycleStopped
}
func (w *Worker) visitSubscriptionPage(ctx context.Context, e enrollment, cursor string, visit func(twitch.EventSubEntry) error) (string, error) {
	if err := w.takeSystemHelix(ctx); err != nil {
		return "", err
	}
	subs, next, err := w.twitch.ListEventSubs(ctx, e.broadcasterID, cursor)
	if err != nil {
		return "", err
	}
	for _, sub := range subs {
		if err := visit(sub); err != nil {
			return "", err
		}
	}
	return next, nil
}
