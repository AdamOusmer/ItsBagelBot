package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"errors"
	"time"
)

type lifecycleTransition struct {
	state            *lifecycle.State
	request          usersrpc.LifecycleRequest
	now              time.Time
	serviceBanned    bool
	beforeGeneration int64
}

var lifecycleActions = map[string]func(*lifecycleTransition) (bool, error){
	"seed_ack":           (*lifecycleTransition).acknowledgeSeed,
	"seed":               (*lifecycleTransition).seed,
	"seed_blocked":       (*lifecycleTransition).seedBlocked,
	"enable":             (*lifecycleTransition).enable,
	"disable":            (*lifecycleTransition).disable,
	"subscription_owned": (*lifecycleTransition).recordSubscriptions,
	"verified_unban":     (*lifecycleTransition).verifyUnban,
	"ban":                (*lifecycleTransition).observeModeration,
	"unban":              (*lifecycleTransition).observeModeration,
	"timeout":            (*lifecycleTransition).observeModeration,
	"grant_missing":      (*lifecycleTransition).loseGrant,
	"grant_restored":     (*lifecycleTransition).restoreGrant,
	"engage":             (*lifecycleTransition).engage,
	"dormancy_claim":     (*lifecycleTransition).claimDormancy,
	"dormancy_probe":     (*lifecycleTransition).probeDormancy,
	"dormancy_live":      (*lifecycleTransition).postponeLiveDormancy,
	"dormancy_defer":     (*lifecycleTransition).deferDormancy,
	"dormancy":           (*lifecycleTransition).enterDormancy,
	"retry":              (*lifecycleTransition).retryRecovery,
	"degraded_retry":     (*lifecycleTransition).retryDegraded,
	"claim":              (*lifecycleTransition).claimRecovery,
	"recovery_success":   (*lifecycleTransition).completeRecovery,
	"recovery_failed":    (*lifecycleTransition).completeRecovery,
	"control_claim":      (*lifecycleTransition).claimControlRepair,
	"control_success":    (*lifecycleTransition).completeControlRepair,
	"control_failed":     (*lifecycleTransition).completeControlRepair,
	"cleanup_claim":      (*lifecycleTransition).claimCleanup,
	"cleanup_success":    (*lifecycleTransition).completeCleanup,
	"cleanup_failed":     (*lifecycleTransition).completeCleanup,
	"notice_ack":         (*lifecycleTransition).acknowledgeNotice,
	"notice_claim":       (*lifecycleTransition).claimNotice,
	"retraction_ack":     (*lifecycleTransition).acknowledgeRetraction,
	"timeout_expired":    (*lifecycleTransition).expireTimeout,
}

func transitionLifecycle(s *lifecycle.State, r usersrpc.LifecycleRequest, now time.Time, serviceBanned bool) (bool, error) {
	apply, found := lifecycleActions[r.Action]
	if !found {
		return false, errors.New("unknown lifecycle action")
	}
	return apply(&lifecycleTransition{state: s, request: r, now: now, serviceBanned: serviceBanned, beforeGeneration: s.AdmissionGeneration})
}
func (t *lifecycleTransition) bump() { t.state.AdmissionGeneration = t.beforeGeneration + 1 }
func (t *lifecycleTransition) incident(reason string) {
	s := t.state
	if s.IncidentID == "" {
		s.IncidentID = lifecycleIdentity()
		s.IncidentAnnounced = false
		s.NoticeDeadline = t.now.Add(24 * time.Hour)
		s.NoticeNextAttemptAt = t.now
	}
	s.IncidentReason = reason
}
func (t *lifecycleTransition) restrict() {
	s := t.state
	if s.GrantHealth == lifecycle.GrantMissing || !s.DesiredEnabled {
		s.ControlRepairAt = time.Time{}
	}
	s.NormalCleanupAt = t.now
	s.NormalCleanupAttempts = 0
	s.OperationID = ""
	s.NextAttemptAt = time.Time{}
	s.RuntimeState = lifecycle.Disabled
	if s.DesiredEnabled {
		s.RuntimeState = lifecycle.Blocked
	}
}
func (t *lifecycleTransition) canRecover() bool {
	if !t.state.DesiredEnabled {
		return false
	}
	if t.serviceBanned {
		return false
	}
	if t.state.ChatRestriction != lifecycle.RestrictionNone {
		return false
	}
	return t.state.GrantHealth != lifecycle.GrantMissing
}
func (t *lifecycleTransition) recover() {
	if !t.canRecover() {
		return
	}
	s := t.state
	s.NormalCleanupAt = time.Time{}
	s.NormalCleanupAttempts = 0
	s.RuntimeState = lifecycle.Recovering
	s.OperationID = lifecycleIdentity()
	s.Attempts = 0
	s.NextAttemptAt = t.now
	t.bump()
}

func (t *lifecycleTransition) sameGeneration() bool {
	return t.request.AdmissionGeneration == t.state.AdmissionGeneration
}
func (t *lifecycleTransition) activeGeneration() bool {
	return t.state.RuntimeState == lifecycle.Active && t.sameGeneration()
}
func (t *lifecycleTransition) recoveryMatches() bool {
	if t.state.RuntimeState != lifecycle.Recovering {
		return false
	}
	if t.request.OperationID != t.state.OperationID {
		return false
	}
	return t.sameGeneration()
}
func (t *lifecycleTransition) hasRecoveryIdentity() bool {
	return t.request.OperationID != "" && t.recoveryMatches()
}
func (t *lifecycleTransition) freshGrant() bool {
	r := t.request
	if r.ObservedAt.IsZero() {
		return false
	}
	if r.EventID == "" {
		return false
	}
	if !t.sameGeneration() {
		return false
	}
	return r.EventID != t.state.GrantEventID
}
