package worker

import (
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/lifecycle"
	notificationsrpc "ItsBagelBot/internal/domain/rpc/notifications"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"context"
	"strconv"
	"time"
)

func (w *Worker) reconcileNotice(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) {
	if w.reauth == nil || w.lifecycleNC == nil {
		return
	}
	stuck := w.retractNotice(ctx, uid, current)
	switch {
	case noticePending(current.State):
		w.deliverNotice(ctx, uid, current)
	case stuck:
		_, _ = w.lifecycleRequest(ctx, noticeRequest(uid, current, "notice_claim"))
	}
}

func noticePending(s lifecycle.State) bool {
	return s.IncidentID != "" && !s.IncidentAnnounced
}

func (w *Worker) deliverNotice(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) {
	req := noticeRequest(uid, current, "notice_claim")
	if !w.noticeClaimed(ctx, req) {
		return
	}
	if w.sendNotice(ctx, uid, current) {
		req.Action = "notice_ack"
		_, _ = w.lifecycleRequest(ctx, req)
	}
}

func (w *Worker) noticeClaimed(ctx context.Context, req usersrpc.LifecycleRequest) bool {
	claimed, err := w.lifecycleRequest(ctx, req)
	return err == nil && claimed.Applied && !claimed.State.IncidentAnnounced
}

func noticeRequest(uid uint64, current usersrpc.LifecycleReply, action string) usersrpc.LifecycleRequest {
	return usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: current.State.AdmissionGeneration, Action: action, IncidentID: current.State.IncidentID}
}
func (w *Worker) retractNotice(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) bool {
	s := current.State
	if s.RetractionIncidentID == "" {
		return false
	}
	request := notificationsrpc.ConnectionRequest{AccountID: uid, AccountCreatedAt: current.AccountCreatedAt, IncidentID: s.RetractionIncidentID, DeliveryDeadline: s.RetractionDeadline, ExpiresAt: s.RetractionExpiresAt}
	result, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".retract", request, 3*time.Second)
	if err != nil || result.Outcome != "retracted" {
		return true
	}
	req := noticeRequest(uid, current, "retraction_ack")
	req.IncidentID = s.RetractionIncidentID
	_, _ = w.lifecycleRequest(ctx, req)
	return false
}
func (w *Worker) sendNotice(ctx context.Context, uid uint64, current usersrpc.LifecycleReply) bool {
	locale, found := w.reauth.ResolveLocale(ctx, strconv.FormatUint(uid, 10))
	if !found {
		return false
	}
	s := current.State
	n := noticeRevoked
	if s.ChatRestriction == lifecycle.RestrictionBanned {
		n = noticeBanned
	}
	request := notificationsrpc.ConnectionRequest{AccountID: uid, AccountCreatedAt: current.AccountCreatedAt, IncidentID: s.IncidentID, DeliveryDeadline: s.NoticeDeadline, ExpiresAt: s.NoticeDeadline.Add(90 * 24 * time.Hour), Title: i18n.T(locale, n.title), Body: i18n.T(locale, n.body), Level: "warning"}
	result, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".send", request, 3*time.Second)
	if err != nil {
		return false
	}
	return terminalNoticeOutcome(result.Outcome)
}
func terminalNoticeOutcome(outcome string) bool {
	switch outcome {
	case "accepted", "suppressed", "expired":
		return true
	}
	return false
}
