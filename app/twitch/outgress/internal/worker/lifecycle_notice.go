package worker

import (
	"ItsBagelBot/internal/domain/i18n"
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
	s := current.State
	r := usersrpc.LifecycleRequest{UserID: uid, AccountCreatedAt: current.AccountCreatedAt, AdmissionGeneration: s.AdmissionGeneration, Action: "notice_claim", IncidentID: s.IncidentID}
	if s.RetractionIncidentID != "" {
		outcome, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".retract", notificationsrpc.ConnectionRequest{AccountID: uid, AccountCreatedAt: current.AccountCreatedAt, IncidentID: s.RetractionIncidentID, DeliveryDeadline: s.RetractionDeadline, ExpiresAt: s.RetractionExpiresAt}, 3*time.Second)
		if err == nil && outcome.Outcome == "retracted" {
			r.Action = "retraction_ack"
			r.IncidentID = s.RetractionIncidentID
			_, _ = w.lifecycleRequest(ctx, r)
		}
	}
	r.Action = "notice_claim"
	r.IncidentID = s.IncidentID
	if s.IncidentID == "" || s.IncidentAnnounced {
		return
	}
	claimed, err := w.lifecycleRequest(ctx, r)
	if err != nil || !claimed.Applied {
		return
	}
	n := noticeRevoked
	if s.ChatRestriction != "none" {
		n = noticeBanned
	}
	locale, found := w.reauth.ResolveLocale(ctx, strconv.FormatUint(uid, 10))
	if !found {
		return
	}
	result, err := bus.RequestJSONTimeout[notificationsrpc.ConnectionReply](ctx, w.lifecycleNC, notificationsrpc.ConnectionSubject+".send", notificationsrpc.ConnectionRequest{AccountID: uid, AccountCreatedAt: current.AccountCreatedAt, IncidentID: s.IncidentID, DeliveryDeadline: s.NoticeDeadline, ExpiresAt: s.NoticeDeadline.Add(90 * 24 * time.Hour), Title: i18n.T(locale, n.title), Body: i18n.T(locale, n.body), Level: "warning"}, 3*time.Second)
	if err == nil && (result.Outcome == "accepted" || result.Outcome == "suppressed" || result.Outcome == "expired") {
		r.Action = "notice_ack"
		_, _ = w.lifecycleRequest(ctx, r)
	}
}
