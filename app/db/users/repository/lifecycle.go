package repository

import (
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/lifecycleerasure"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

func lifecycleIdentity() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// LegacyLifecycle derives a read-only rollout view. Never use a missing account
// or unavailable database as a legacy enabled account.
func lifecycleView(u *ent.User) *lifecycle.State {
	s := LegacyLifecycle(u, time.Now().UTC())
	return &s
}

func LegacyLifecycle(u *ent.User, now time.Time) lifecycle.State {
	if u.Lifecycle != nil {
		return *u.Lifecycle
	}
	s := lifecycle.State{Version: 1, DesiredEnabled: u.IsActive, RuntimeState: lifecycle.Disabled, AdmissionGeneration: 1, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, LastEngagedAt: now}
	if u.IsActive && !u.Banned {
		s.RuntimeState = lifecycle.Active
	}
	return s
}

func (r *Users) setDesiredEnabled(ctx context.Context, id uint64, enabled bool) error {
	action := "disable"
	if enabled {
		action = "enable"
	}
	_, err := r.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: id, Action: action})
	return err
}

// Lifecycle uses revision CAS so competing replicas never apply an old ban,
// owner decision or recovery result over newer account state. The JSON current
// record and its indexed due dates commit in the same SQL statement.
func (r *Users) Lifecycle(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	if req.Action == "erase_claim" || req.Action == "erase_ack" {
		return r.lifecycleErase(ctx, req)
	}
	if req.Action == "due" {
		return r.lifecycleDue(ctx)
	}
	for attempt := 0; attempt < 8; attempt++ {
		row, err := r.client.User.Get(ctx, req.UserID)
		if err != nil {
			return usersrpc.LifecycleReply{}, err
		}
		now := time.Now().UTC()
		s := LegacyLifecycle(row, now)
		if row.Lifecycle == nil && req.Action == "engage" && !req.ObservedAt.IsZero() {
			s.LastEngagedAt = req.ObservedAt.Add(-time.Nanosecond)
		}
		reply := usersrpc.LifecycleReply{State: s, AccountCreatedAt: row.CreatedAt.UnixMicro(), StateRevision: row.StateRevision}
		if req.AccountCreatedAt != 0 && req.AccountCreatedAt != reply.AccountCreatedAt {
			return reply, nil
		}
		if req.Action == "get" {
			return reply, nil
		}
		if req.Action == "notice_eligible" {
			reply.Applied = req.AccountCreatedAt == reply.AccountCreatedAt && req.IncidentID != "" && req.IncidentID == s.IncidentID && !s.IncidentAnnounced && s.DesiredEnabled
			return reply, nil
		}
		// Internal event writes must always bind to an account incarnation. Owner
		// controls alone obtain their identity from the freshly loaded account.
		if req.Action != "enable" && req.Action != "disable" && req.AccountCreatedAt == 0 {
			return reply, errors.New("lifecycle observation requires account incarnation")
		}
		changed, err := transitionLifecycle(&s, req, now, row.Banned)
		if err != nil {
			return reply, err
		}
		if !changed {
			return reply, nil
		}
		update := r.client.User.Update().Where(user.IDEQ(req.UserID), user.CreatedAtEQ(row.CreatedAt), user.StateRevisionEQ(row.StateRevision)).SetLifecycle(&s).SetLifecycleProjectionPending(true).SetLifecycleState(s.RuntimeState).SetIsActive(s.DesiredEnabled && !row.Banned && s.ChatRestriction == lifecycle.RestrictionNone && s.GrantHealth == lifecycle.GrantHealthy && (s.RuntimeState == lifecycle.Active || s.RuntimeState == lifecycle.Dormant))
		if req.Action == "seed" {
			update.SetLifecycleMigrationPending(true)
		} else if req.Action == "seed_ack" {
			update.SetLifecycleMigrationPending(false)
		}
		if s.RuntimeState == lifecycle.Active {
			deadline := s.LastEngagedAt.Add(lifecycle.DormancyPeriod)
			if s.DormancyAfter.After(deadline) {
				deadline = s.DormancyAfter
			}
			update.SetDormantAfter(deadline)
		} else {
			update.ClearDormantAfter()
		}
		dueAt := s.NextAttemptAt
		for _, at := range []time.Time{s.NormalCleanupAt, s.TimeoutUntil, s.ControlRepairAt} {
			if !at.IsZero() && (dueAt.IsZero() || at.Before(dueAt)) {
				dueAt = at
			}
		}
		if dueAt.IsZero() {
			update.ClearLifecycleDueAt()
		} else {
			update.SetLifecycleDueAt(dueAt)
		}

		if !s.NoticeNextAttemptAt.IsZero() {
			update.SetNoticeDueAt(s.NoticeNextAttemptAt)
		} else {
			update.ClearNoticeDueAt()
		}
		n, err := update.Save(ctx)
		if err != nil {
			return reply, err
		}
		if n == 0 {
			continue
		}
		// Required work is in the current record before publishing. A crash or
		// unavailable bus leaves the indexed due marker for bounded replay.
		if err = r.publishChanged(ctx, req.UserID); err != nil {
			return usersrpc.LifecycleReply{}, err
		}
		if err = r.publishStatusInvalidation(ctx, req.UserID); err != nil {
			return usersrpc.LifecycleReply{}, err
		}
		_, err = r.client.User.Update().Where(user.IDEQ(req.UserID), user.CreatedAtEQ(row.CreatedAt), user.StateRevisionEQ(row.StateRevision+1)).SetLifecycleProjectionPending(false).Save(ctx)
		if err != nil {
			return usersrpc.LifecycleReply{}, err
		}
		committed, err := r.client.User.Get(ctx, req.UserID)
		if err != nil {
			return usersrpc.LifecycleReply{}, err
		}
		return usersrpc.LifecycleReply{State: *committed.Lifecycle, AccountCreatedAt: committed.CreatedAt.UnixMicro(), StateRevision: committed.StateRevision, Applied: true}, nil
	}
	return usersrpc.LifecycleReply{}, errors.New("concurrent lifecycle mutation; retry")
}

func transitionLifecycle(s *lifecycle.State, r usersrpc.LifecycleRequest, now time.Time, serviceBanned bool) (bool, error) {
	beforeGeneration := s.AdmissionGeneration
	bump := func() { s.AdmissionGeneration = beforeGeneration + 1 }
	incident := func(reason string) {
		if s.IncidentID == "" {
			s.IncidentID = lifecycleIdentity()
			s.IncidentAnnounced = false
			s.NoticeDeadline = now.Add(24 * time.Hour)
			s.NoticeNextAttemptAt = now
		}
		s.IncidentReason = reason
	}
	restrict := func() {
		if s.GrantHealth == lifecycle.GrantMissing || !s.DesiredEnabled {
			s.ControlRepairAt = time.Time{}
		}
		s.NormalCleanupAt = now
		s.NormalCleanupAttempts = 0
		s.OperationID = ""
		s.NextAttemptAt = time.Time{}
		if s.DesiredEnabled {
			s.RuntimeState = lifecycle.Blocked
		} else {
			s.RuntimeState = lifecycle.Disabled
		}
	}
	recover := func() {
		if !s.DesiredEnabled || serviceBanned || s.ChatRestriction != lifecycle.RestrictionNone || s.GrantHealth == lifecycle.GrantMissing {
			return
		}
		s.NormalCleanupAt = time.Time{}
		s.NormalCleanupAttempts = 0
		s.RuntimeState = lifecycle.Recovering
		s.OperationID = lifecycleIdentity()
		s.Attempts = 0
		s.NextAttemptAt = now
		bump()
	}
	switch r.Action {
	case "seed_ack":
		return true, nil
	case "seed":
		return true, nil
	case "seed_blocked":
		if r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if s.IncidentID != "" {
			return false, nil
		}
		switch r.Reason {
		case "bot_banned":
			s.ChatRestriction = lifecycle.RestrictionBanned
		case "grant_missing":
			// Migration imports an existing verified registry fact, not a new
			// provider event. Keep the grant-event watermark for real callbacks.
			s.GrantHealth = lifecycle.GrantMissing
		default:
			return false, errors.New("unverified migration reason")
		}
		incident(r.Reason)
		s.IncidentAnnounced = true
		s.NoticeNextAttemptAt = time.Time{}
		restrict()
		bump()
	case "enable":
		if s.DesiredEnabled && s.RuntimeState == lifecycle.Active {
			return false, nil
		}
		s.DesiredEnabled = true
		bump()
		if s.ChatRestriction != lifecycle.RestrictionNone || s.GrantHealth == lifecycle.GrantMissing {
			restrict()
		} else {
			recover()
		}
	case "disable":
		s.NormalCleanupAt = now
		s.NormalCleanupAttempts = 0
		s.ControlRepairAt = time.Time{}
		if !s.DesiredEnabled && s.RuntimeState == lifecycle.Disabled {
			return false, nil
		}
		s.DesiredEnabled = false
		if s.IncidentID != "" && !s.IncidentAnnounced {
			s.IncidentAnnounced = true
			s.NoticeNextAttemptAt = time.Time{}
		}
		s.RuntimeState = lifecycle.Disabled
		s.OperationID = ""
		s.NextAttemptAt = time.Time{}
		bump()
	case "subscription_owned":
		if r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if r.SubscriptionID != "" {
			s.ChatSubscriptionID = r.SubscriptionID
		}
		if r.SubscriptionIDs != nil {
			s.OwnedSubscriptions = append([]string(nil), r.SubscriptionIDs...)
		}
		if r.ControlListenersHealthy != nil {
			s.ControlListenersHealthy = *r.ControlListenersHealthy
			if *r.ControlListenersHealthy || r.Reason == "listener_permission" {
				s.ControlRepairAt = time.Time{}
				s.ControlRepairAttempts = 0
			} else if r.Reason == "listener_transient" || r.Reason == "listener_revoked" {
				if s.ControlRepairAt.IsZero() && s.ControlRepairAttempts < 3 {
					s.ControlRepairAt = now
				}
			}
		}
	case "verified_unban":
		if s.RuntimeState != lifecycle.Recovering || r.OperationID == "" || r.OperationID != s.OperationID || r.AdmissionGeneration != s.AdmissionGeneration || !s.DesiredEnabled || r.ObservedAt.IsZero() || r.ObservedAt.Before(s.ModerationAt) {
			return false, nil
		}
		s.ChatRestriction = lifecycle.RestrictionNone
		s.TimeoutUntil = time.Time{}
		s.ModerationAt = r.ObservedAt
		s.ModerationEventID = "verified:" + r.OperationID

	case "ban", "unban", "timeout":
		if r.Action == "ban" && r.SubscriptionID != "" && r.SubscriptionID == s.ChatSubscriptionID && s.ChatRestriction == lifecycle.RestrictionTimeout {
			return false, nil
		}
		if r.SubscriptionID != "" && (s.ChatSubscriptionID == "" || s.ChatSubscriptionID != r.SubscriptionID) {
			return false, nil
		}
		if r.EventID == "" || r.ObservedAt.IsZero() {
			return false, errors.New("moderation observation requires event identity and time")
		}
		if r.ObservedAt.Before(s.ModerationAt) || r.EventID == s.ModerationEventID {
			return false, nil
		}
		// Equal-time opposing observations are ambiguous: remain stopped until a
		// genuinely newer unban. Do not let delivery order authorize runtime.
		if r.ObservedAt.Equal(s.ModerationAt) && r.Action == "unban" && s.ChatRestriction != lifecycle.RestrictionNone {
			return false, nil
		}
		s.ModerationAt = r.ObservedAt
		s.ModerationEventID = r.EventID
		if r.Action == "unban" {
			s.ChatRestriction = lifecycle.RestrictionNone
			s.NormalCleanupAt = time.Time{}
			s.NormalCleanupAttempts = 0
			s.TimeoutUntil = time.Time{}
			bump()
			recover()
		} else {
			s.ChatRestriction = lifecycle.RestrictionBanned
			s.TimeoutUntil = time.Time{}
			reason := "bot_banned"
			if r.Action == "timeout" {
				if !r.TimeoutUntil.After(r.ObservedAt) {
					return false, errors.New("timeout deadline must follow observation")
				}
				s.ChatRestriction = lifecycle.RestrictionTimeout
				s.TimeoutUntil = r.TimeoutUntil
				reason = "bot_timed_out"
			}
			incident(reason)
			restrict()
			bump()
		}
	case "grant_missing":
		if r.ObservedAt.IsZero() || r.EventID == "" || r.AdmissionGeneration != s.AdmissionGeneration || r.ObservedAt.Before(s.GrantObservedAt) || r.EventID == s.GrantEventID {
			return false, nil
		}
		s.GrantObservedAt = r.ObservedAt
		s.GrantEventID = r.EventID
		if s.GrantHealth == lifecycle.GrantMissing {
			return true, nil
		}
		s.GrantHealth = lifecycle.GrantMissing
		incident("grant_missing")
		restrict()
		bump()
	case "grant_restored":
		if r.ObservedAt.IsZero() || r.EventID == "" || r.AdmissionGeneration != s.AdmissionGeneration || !r.ObservedAt.After(s.GrantObservedAt) || r.EventID == s.GrantEventID {
			return false, nil
		}
		s.GrantObservedAt = r.ObservedAt
		s.GrantEventID = r.EventID
		if s.DesiredEnabled && (s.ChatRestriction != lifecycle.RestrictionNone || !s.ControlListenersHealthy) {
			s.ControlRepairAt = now
			s.ControlRepairAttempts = 0
		}
		if s.GrantHealth == lifecycle.GrantHealthy {
			return true, nil
		}
		s.GrantHealth = lifecycle.GrantHealthy
		bump()
		recover()
	case "engage":
		if !s.AllowsRuntime() || r.EventID == "" || r.EventID == s.LastActivityID || r.ObservedAt.IsZero() || !r.ObservedAt.After(s.LastEngagedAt) {
			return false, nil
		}
		if r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if s.RuntimeState != lifecycle.Dormant && r.ObservedAt.Sub(s.LastEngagedAt) < 15*time.Minute {
			return false, nil
		}
		s.DormancyAfter = time.Time{}
		s.DormancyClaimUntil = time.Time{}
		s.DormancyProbeAt = time.Time{}
		s.DormancyUnknownSince = time.Time{}
		s.LastEngagedAt = r.ObservedAt
		s.LastActivityID = r.EventID
		if s.RuntimeState == lifecycle.Dormant {
			s.RuntimeState = lifecycle.Active
			bump()
		}
	case "dormancy_claim":
		if s.RuntimeState != lifecycle.Active || r.AdmissionGeneration != s.AdmissionGeneration || now.Before(s.LastEngagedAt.Add(lifecycle.DormancyPeriod)) || now.Before(s.DormancyAfter) || now.Before(s.DormancyClaimUntil) {
			return false, nil
		}
		s.DormancyClaimUntil = now.Add(time.Minute)
		s.DormancyAfter = s.DormancyClaimUntil
	case "dormancy_probe":
		if s.RuntimeState != lifecycle.Active || r.AdmissionGeneration != s.AdmissionGeneration || (!s.DormancyProbeAt.IsZero() && now.Sub(s.DormancyProbeAt) < 24*time.Hour) {
			return false, nil
		}
		s.DormancyProbeAt = now
	case "dormancy_live":
		if s.RuntimeState != lifecycle.Active || r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		s.DormancyAfter = now.Add(time.Hour)
		s.DormancyClaimUntil = time.Time{}
		s.DormancyUnknownSince = time.Time{}
	case "dormancy_defer":
		if s.RuntimeState != lifecycle.Active || r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if s.DormancyUnknownSince.IsZero() {
			s.DormancyUnknownSince = now
		}
		s.DormancyAfter = now.Add(time.Hour)
		deadline := s.DormancyUnknownSince.Add(24 * time.Hour)
		if deadline.Before(s.DormancyAfter) {
			s.DormancyAfter = deadline
		}
		if !s.DormancyAfter.After(now) {
			s.DormancyAfter = now.Add(time.Minute)
		}
		s.DormancyClaimUntil = time.Time{}
	case "dormancy":
		if s.RuntimeState != lifecycle.Active || r.AdmissionGeneration != s.AdmissionGeneration || now.Before(s.LastEngagedAt.Add(lifecycle.DormancyPeriod)) {
			return false, nil
		}
		if r.Reason != "offline" && (r.Reason != "unavailable" || s.DormancyUnknownSince.IsZero() || now.Before(s.DormancyUnknownSince.Add(24*time.Hour))) {
			return false, nil
		}
		s.RuntimeState = lifecycle.Dormant
		s.DormancyAfter = time.Time{}
		s.DormancyClaimUntil = time.Time{}
		bump()

	case "retry":
		if s.RuntimeState == lifecycle.Recovering || !s.DesiredEnabled || serviceBanned {
			return false, nil
		}
		// Explicit verification can inspect a retained ban; it never clears the fact
		// or authorizes effects until fenced positive verification succeeds.
		s.RuntimeState = lifecycle.Recovering
		s.OperationID = lifecycleIdentity()
		s.Attempts = 0
		s.NextAttemptAt = now
		bump()

	case "claim":
		if s.RuntimeState != lifecycle.Recovering || r.OperationID != s.OperationID || r.AdmissionGeneration != s.AdmissionGeneration || now.Before(s.NextAttemptAt) {
			return false, nil
		}
		if s.Attempts >= lifecycle.MaxRecoveryAttempts {
			s.RuntimeState = lifecycle.Degraded
			s.NextAttemptAt = time.Time{}
			bump()
		} else {
			s.Attempts++
			s.NextAttemptAt = now.Add(time.Minute)
		}
	case "recovery_success", "recovery_failed":
		if s.RuntimeState != lifecycle.Recovering || r.OperationID == "" || r.OperationID != s.OperationID || r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if r.Action == "recovery_success" {
			if s.IncidentID != "" && len(s.Retractions) >= 8 {
				return false, nil
			}
			if !s.DesiredEnabled || serviceBanned || s.ChatRestriction != lifecycle.RestrictionNone || s.GrantHealth == lifecycle.GrantMissing {
				return false, nil
			}
			s.GrantHealth = lifecycle.GrantHealthy
			s.RuntimeState = lifecycle.Active
			s.LastEngagedAt = now
			s.OperationID = ""
			s.NextAttemptAt = time.Time{}
			if s.IncidentID != "" {
				s.Retractions = append(s.Retractions, lifecycle.Retraction{IncidentID: s.IncidentID, DeliveryDeadline: s.NoticeDeadline, ExpiresAt: s.NoticeDeadline.Add(90 * 24 * time.Hour)})
				if s.RetractionIncidentID == "" {
					s.RetractionIncidentID = s.IncidentID
					s.RetractionDeadline = s.NoticeDeadline
					s.RetractionExpiresAt = s.NoticeDeadline.Add(90 * 24 * time.Hour)
				}
				s.NoticeNextAttemptAt = now
			}
			s.IncidentID = ""
			s.IncidentReason = ""
			s.IncidentAnnounced = false
			s.LastFailure = ""
			bump()
		} else {
			s.LastFailure = "connection_check_failed"
			if s.Attempts >= lifecycle.MaxRecoveryAttempts {
				s.RuntimeState = lifecycle.Degraded
				s.NextAttemptAt = time.Time{}
				bump()
			} else {
				s.NextAttemptAt = now.Add(time.Duration(s.Attempts+1) * time.Minute)
			}
		}
	case "control_claim":
		if r.AdmissionGeneration != s.AdmissionGeneration || !s.DesiredEnabled || s.GrantHealth != lifecycle.GrantHealthy || s.ControlRepairAt.IsZero() || now.Before(s.ControlRepairAt) || s.ControlRepairAttempts >= 3 {
			return false, nil
		}
		s.ControlRepairAttempts++
		s.ControlRepairAt = now.Add(time.Minute)
	case "control_success", "control_failed":
		if r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		if r.Action == "control_success" || s.ControlRepairAttempts >= 3 {
			s.ControlRepairAt = time.Time{}
		} else {
			s.ControlRepairAt = now.Add(time.Duration(s.ControlRepairAttempts+1) * time.Minute)
		}
	case "cleanup_claim":
		if r.AdmissionGeneration != s.AdmissionGeneration || s.NormalCleanupAt.IsZero() || now.Before(s.NormalCleanupAt) {
			return false, nil
		}
		if s.NormalCleanupAttempts < 3 {
			s.NormalCleanupAttempts++
		}
		s.NormalCleanupAt = now.Add(time.Minute)
	case "cleanup_success", "cleanup_failed":
		if r.AdmissionGeneration != s.AdmissionGeneration || s.NormalCleanupAt.IsZero() {
			return false, nil
		}
		if r.Action == "cleanup_success" {
			s.NormalCleanupAt = time.Time{}
			s.NormalCleanupAttempts = 0
		} else if s.NormalCleanupAttempts >= 3 {
			s.NormalCleanupAt = now.Add(24 * time.Hour)
		} else {
			s.NormalCleanupAt = now.Add(time.Duration(s.NormalCleanupAttempts+1) * time.Minute)
		}
	case "notice_ack":
		if r.IncidentID == "" || r.IncidentID != s.IncidentID || s.IncidentAnnounced {
			return false, nil
		}
		s.IncidentAnnounced = true
		if s.RetractionIncidentID == "" {
			s.NoticeNextAttemptAt = time.Time{}
		}
	case "notice_claim":
		if now.Before(s.NoticeNextAttemptAt) || (s.IncidentID == "" && s.RetractionIncidentID == "") {
			return false, nil
		}
		s.NoticeNextAttemptAt = now.Add(time.Minute)
	case "retraction_ack":
		if r.IncidentID == "" || r.IncidentID != s.RetractionIncidentID {
			return false, nil
		}
		s.RetractionIncidentID = ""
		if len(s.Retractions) > 0 {
			s.Retractions = append([]lifecycle.Retraction(nil), s.Retractions[1:]...)
			if len(s.Retractions) > 0 {
				s.RetractionIncidentID = s.Retractions[0].IncidentID
				s.RetractionDeadline = s.Retractions[0].DeliveryDeadline
				s.RetractionExpiresAt = s.Retractions[0].ExpiresAt
			}
		}
		if s.RetractionIncidentID != "" {
			s.NoticeNextAttemptAt = now
		}
		if s.RetractionIncidentID == "" && (s.IncidentID == "" || s.IncidentAnnounced) {
			s.NoticeNextAttemptAt = time.Time{}
		}
	case "timeout_expired":
		if s.ChatRestriction != lifecycle.RestrictionTimeout || now.Before(s.TimeoutUntil) || r.AdmissionGeneration != s.AdmissionGeneration {
			return false, nil
		}
		// Expiry permits verification only; it does not itself restore admission.
		s.ChatRestriction = lifecycle.RestrictionNone
		s.TimeoutUntil = time.Time{}
		bump()
		recover()
	default:
		return false, errors.New("unknown lifecycle action")
	}
	return true, nil
}

func (r *Users) lifecycleDue(ctx context.Context) (usersrpc.LifecycleReply, error) {
	now := time.Now().UTC()
	rows, err := r.client.User.Query().Where(user.Or(user.And(user.LifecycleStateEQ(lifecycle.Active), user.DormantAfterLTE(now)), user.LifecycleDueAtLTE(now), user.NoticeDueAtLTE(now), user.LifecycleProjectionPendingEQ(true))).Order(ent.Asc(user.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	reply := usersrpc.LifecycleReply{}
	seeds, err := r.client.User.Query().Where(user.LifecycleStateEQ(""), user.LifecycleIsNil()).Order(ent.Asc(user.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return reply, err
	}
	for _, row := range seeds {
		result, err := r.Lifecycle(ctx, usersrpc.LifecycleRequest{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), Action: "seed"})
		if err != nil {
			return reply, err
		}
		_ = result
	}
	pendingSeeds, err := r.client.User.Query().Where(user.LifecycleMigrationPendingEQ(true)).Order(ent.Asc(user.FieldID)).Limit(200).All(ctx)
	if err != nil {
		return reply, err
	}
	for _, row := range pendingSeeds {
		if row.Lifecycle != nil {
			reply.Seeded = append(reply.Seeded, usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: *row.Lifecycle})
		}
	}
	for _, row := range rows {
		if row.LifecycleProjectionPending {
			if err := r.publishChanged(ctx, row.ID); err != nil {
				return reply, err
			}
			if err := r.publishStatusInvalidation(ctx, row.ID); err != nil {
				return reply, err
			}
			if _, err := r.client.User.Update().Where(user.IDEQ(row.ID), user.CreatedAtEQ(row.CreatedAt), user.StateRevisionEQ(row.StateRevision)).SetLifecycleProjectionPending(false).Save(ctx); err != nil {
				return reply, err
			}
		}
		s := row.Lifecycle
		if s == nil {
			continue
		}
		req := usersrpc.LifecycleRequest{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), AdmissionGeneration: s.AdmissionGeneration}
		if s.RuntimeState == lifecycle.Active && row.DormantAfter != nil && !now.Before(*row.DormantAfter) {
			reply.Dormancy = append(reply.Dormancy, usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: *s})
		}
		if s.ChatRestriction == lifecycle.RestrictionTimeout {
			req.Action = "timeout_expired"
		}
		if req.Action != "" {
			result, err := r.Lifecycle(ctx, req)
			if err != nil {
				return reply, err
			}
			s = &result.State
		}
		if !s.ControlRepairAt.IsZero() && !now.Before(s.ControlRepairAt) {
			reply.ControlRepair = append(reply.ControlRepair, usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: *s})
		}
		if !s.NormalCleanupAt.IsZero() && !now.Before(s.NormalCleanupAt) {
			reply.Cleanup = append(reply.Cleanup, usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: *s})
		}
		if s.RuntimeState == lifecycle.Recovering || (!s.NoticeNextAttemptAt.IsZero() && !now.Before(s.NoticeNextAttemptAt)) {
			reply.Items = append(reply.Items, usersrpc.LifecycleWork{UserID: row.ID, AccountCreatedAt: row.CreatedAt.UnixMicro(), State: *s})
		}
	}
	if err := r.erasureDue(ctx, &reply); err != nil {
		return reply, err
	}
	return reply, nil
}

func (r *Users) erasureDue(ctx context.Context, reply *usersrpc.LifecycleReply) error {
	now := time.Now().UTC()
	rows, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.CompletedAtIsNil(), lifecycleerasure.NextAttemptAtLTE(now)).Order(ent.Asc(lifecycleerasure.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var state lifecycle.State
		if row.Lifecycle != nil {
			state = *row.Lifecycle
		}
		reply.Erasures = append(reply.Erasures, usersrpc.LifecycleErasure{UserID: row.UserID, AccountCreatedAt: row.AccountCreatedAt, DeletedAt: row.DeletedAt, State: state, NotificationsDone: row.NotificationsDone, OutgressDone: row.OutgressDone})
	}
	expired, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.CompletedAtLT(now.Add(-7 * 24 * time.Hour))).Order(ent.Asc(lifecycleerasure.FieldID)).Limit(100).All(ctx)
	if err != nil {
		return err
	}
	ids := make([]int, 0, len(expired))
	for _, row := range expired {
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		_, err = r.client.LifecycleErasure.Delete().Where(lifecycleerasure.IDIn(ids...)).Exec(ctx)
	}
	return err
}
func (r *Users) lifecycleErase(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, error) {
	if req.AccountCreatedAt == 0 {
		return usersrpc.LifecycleReply{}, errors.New("erasure requires incarnation")
	}
	row, err := r.client.LifecycleErasure.Query().Where(lifecycleerasure.UserIDEQ(req.UserID), lifecycleerasure.AccountCreatedAtEQ(req.AccountCreatedAt)).Only(ctx)
	if err != nil {
		return usersrpc.LifecycleReply{}, err
	}
	now := time.Now().UTC()
	reply := usersrpc.LifecycleReply{AccountCreatedAt: row.AccountCreatedAt}
	if row.CompletedAt != nil {
		return reply, nil
	}
	if req.Action == "erase_claim" {
		n, err := r.client.LifecycleErasure.Update().Where(lifecycleerasure.IDEQ(row.ID), lifecycleerasure.NextAttemptAtLTE(now), lifecycleerasure.CompletedAtIsNil()).SetNextAttemptAt(now.Add(time.Minute)).Save(ctx)
		reply.Applied = n == 1
		return reply, err
	}
	update := r.client.LifecycleErasure.UpdateOneID(row.ID)
	switch req.Reason {
	case "notifications":
		update.SetNotificationsDone(true)
	case "outgress":
		update.SetOutgressDone(true)
	default:
		return reply, errors.New("unknown erasure participant")
	}
	committed, err := update.Save(ctx)
	if err != nil {
		return reply, err
	}
	if committed.NotificationsDone && committed.OutgressDone {
		err = r.client.LifecycleErasure.UpdateOneID(row.ID).SetCompletedAt(now).Exec(ctx)
	}
	reply.Applied = true
	return reply, err
}
