package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"time"
)

func (t *lifecycleTransition) retryRecovery() (bool, error) {
	s := t.state
	if t.recoveryRetryBlocked() {
		return false, nil
	}
	// Explicit verification can inspect a retained ban; it never clears the fact
	// or authorizes effects until fenced positive verification succeeds.
	s.RuntimeState = lifecycle.Recovering
	s.OperationID = lifecycleIdentity()
	s.Attempts = 0
	s.NextAttemptAt = t.now
	t.bump()
	return true, nil
}

func (t *lifecycleTransition) claimRecovery() (bool, error) {
	s := t.state

	if !t.recoveryMatches() || t.now.Before(s.NextAttemptAt) {
		return false, nil
	}
	if s.Attempts >= lifecycle.MaxRecoveryAttempts {
		s.RuntimeState = lifecycle.Degraded
		s.NextAttemptAt = time.Time{}
		t.bump()
	} else {
		s.Attempts++
		s.NextAttemptAt = t.now.Add(time.Minute)
	}
	return true, nil
}

func (t *lifecycleTransition) completeRecovery() (bool, error) {
	if !t.hasRecoveryIdentity() {
		return false, nil
	}
	if t.request.Action == "recovery_failed" {
		return t.failRecovery()
	}
	return t.succeedRecovery()
}
func (t *lifecycleTransition) succeedRecovery() (bool, error) {
	if t.state.IncidentID != "" && len(t.state.Retractions) >= 8 {
		return false, nil
	}
	if !t.canRecover() {
		return false, nil
	}
	s := t.state
	s.GrantHealth = lifecycle.GrantHealthy
	s.RuntimeState = lifecycle.Active
	s.LastEngagedAt = t.now
	s.OperationID = ""
	s.NextAttemptAt = time.Time{}
	t.retractIncident()
	s.IncidentID = ""
	s.IncidentReason = ""
	s.IncidentAnnounced = false
	s.LastFailure = ""
	t.bump()
	return true, nil
}
func (t *lifecycleTransition) retractIncident() {
	s := t.state
	if s.IncidentID == "" {
		return
	}
	s.Retractions = append(s.Retractions, lifecycle.Retraction{IncidentID: s.IncidentID, DeliveryDeadline: s.NoticeDeadline, ExpiresAt: s.NoticeDeadline.Add(90 * 24 * time.Hour)})
	if s.RetractionIncidentID == "" {
		s.RetractionIncidentID = s.IncidentID
		s.RetractionDeadline = s.NoticeDeadline
		s.RetractionExpiresAt = s.NoticeDeadline.Add(90 * 24 * time.Hour)
	}
	s.NoticeNextAttemptAt = t.now
}
func (t *lifecycleTransition) failRecovery() (bool, error) {
	s := t.state
	s.LastFailure = "connection_check_failed"
	if s.Attempts >= lifecycle.MaxRecoveryAttempts {
		t.degradeRecovery()
	} else {
		s.NextAttemptAt = t.now.Add(time.Duration(s.Attempts+1) * time.Minute)
	}
	return true, nil
}
func (t *lifecycleTransition) degradeRecovery() {
	t.state.RuntimeState = lifecycle.Degraded
	t.state.NextAttemptAt = time.Time{}
	t.bump()
}

func (t *lifecycleTransition) claimControlRepair() (bool, error) {
	s := t.state

	if !t.controlClaimAllowed() {
		return false, nil
	}
	s.ControlRepairAttempts++
	s.ControlRepairAt = t.now.Add(time.Minute)
	return true, nil
}

func (t *lifecycleTransition) completeControlRepair() (bool, error) {
	s := t.state
	r := t.request

	if r.AdmissionGeneration != s.AdmissionGeneration {
		return false, nil
	}
	if r.Action == "control_success" || s.ControlRepairAttempts >= 3 {
		s.ControlRepairAt = time.Time{}
	} else {
		s.ControlRepairAt = t.now.Add(time.Duration(s.ControlRepairAttempts+1) * time.Minute)
	}
	return true, nil
}

func (t *lifecycleTransition) claimCleanup() (bool, error) {
	s := t.state

	if !t.cleanupClaimAllowed() {
		return false, nil
	}
	if s.NormalCleanupAttempts < 3 {
		s.NormalCleanupAttempts++
	}
	s.NormalCleanupAt = t.now.Add(time.Minute)
	return true, nil
}

func (t *lifecycleTransition) completeCleanup() (bool, error) {
	s := t.state
	r := t.request

	if r.AdmissionGeneration != s.AdmissionGeneration || s.NormalCleanupAt.IsZero() {
		return false, nil
	}
	if r.Action == "cleanup_success" {
		s.NormalCleanupAt = time.Time{}
		s.NormalCleanupAttempts = 0
	} else if s.NormalCleanupAttempts >= 3 {
		s.NormalCleanupAt = t.now.Add(24 * time.Hour)
	} else {
		s.NormalCleanupAt = t.now.Add(time.Duration(s.NormalCleanupAttempts+1) * time.Minute)
	}
	return true, nil
}

func (t *lifecycleTransition) recoveryRetryBlocked() bool {
	if t.state.RuntimeState == lifecycle.Recovering {
		return true
	}
	return !t.state.DesiredEnabled || t.serviceBanned
}
func (t *lifecycleTransition) controlClaimAllowed() bool {
	if !t.sameGeneration() {
		return false
	}
	if !t.state.DesiredEnabled {
		return false
	}
	if t.state.GrantHealth != lifecycle.GrantHealthy {
		return false
	}
	if t.state.ControlRepairAt.IsZero() {
		return false
	}
	if t.now.Before(t.state.ControlRepairAt) {
		return false
	}
	return t.state.ControlRepairAttempts < 3
}
func (t *lifecycleTransition) cleanupClaimAllowed() bool {
	if !t.sameGeneration() {
		return false
	}
	if t.state.NormalCleanupAt.IsZero() {
		return false
	}
	return !t.now.Before(t.state.NormalCleanupAt)
}
