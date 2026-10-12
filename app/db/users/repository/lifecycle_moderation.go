package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"errors"
	"time"
)

func (t *lifecycleTransition) recordSubscriptions() (bool, error) {
	if !t.sameGeneration() {
		return false, nil
	}
	s, r := t.state, t.request
	if r.SubscriptionID != "" {
		s.ChatSubscriptionID = r.SubscriptionID
	}
	if r.SubscriptionIDs != nil {
		s.OwnedSubscriptions = append([]string(nil), r.SubscriptionIDs...)
	}
	t.recordControlHealth()
	return true, nil
}
func (t *lifecycleTransition) recordControlHealth() {
	r := t.request
	if r.ControlListenersHealthy == nil {
		return
	}
	s := t.state
	s.ControlListenersHealthy = *r.ControlListenersHealthy
	if *r.ControlListenersHealthy || r.Reason == "listener_permission" {
		s.ControlRepairAt = time.Time{}
		s.ControlRepairAttempts = 0
		return
	}
	if r.Reason == "listener_transient" || r.Reason == "listener_revoked" {
		t.scheduleControlRepair()
	}
}
func (t *lifecycleTransition) scheduleControlRepair() {
	if t.state.ControlRepairAt.IsZero() && t.state.ControlRepairAttempts < 3 {
		t.state.ControlRepairAt = t.now
	}
}

func (t *lifecycleTransition) verifyUnban() (bool, error) {
	if !t.hasRecoveryIdentity() {
		return false, nil
	}
	if !t.state.DesiredEnabled {
		return false, nil
	}
	if t.request.ObservedAt.IsZero() {
		return false, nil
	}
	if t.request.ObservedAt.Before(t.state.ModerationAt) {
		return false, nil
	}
	s, r := t.state, t.request
	s.ChatRestriction = lifecycle.RestrictionNone
	s.TimeoutUntil = time.Time{}
	s.ModerationAt = r.ObservedAt
	s.ModerationEventID = "verified:" + r.OperationID
	return true, nil
}

func (t *lifecycleTransition) observeModeration() (bool, error) {
	allowed, err := t.acceptsModeration()
	if err != nil || !allowed {
		return false, err
	}
	s, r := t.state, t.request
	s.ModerationAt = r.ObservedAt
	s.ModerationEventID = r.EventID
	if r.Action == "unban" {
		return t.removeBan()
	}
	return t.applyRestriction()
}
func (t *lifecycleTransition) acceptsModeration() (bool, error) {
	if t.timeoutFallback() {
		return false, nil
	}
	if !t.ownsObservationSubscription() {
		return false, nil
	}
	if t.request.EventID == "" || t.request.ObservedAt.IsZero() {
		return false, errors.New("moderation observation requires event identity and time")
	}
	if t.request.ObservedAt.Before(t.state.ModerationAt) || t.request.EventID == t.state.ModerationEventID {
		return false, nil
	}
	if t.ambiguousUnban() {
		return false, nil
	}
	return true, nil
}
func (t *lifecycleTransition) timeoutFallback() bool {
	if t.request.Action != "ban" {
		return false
	}
	if t.request.SubscriptionID == "" {
		return false
	}
	return t.request.SubscriptionID == t.state.ChatSubscriptionID && t.state.ChatRestriction == lifecycle.RestrictionTimeout
}
func (t *lifecycleTransition) ownsObservationSubscription() bool {
	if t.request.SubscriptionID == "" {
		return true
	}
	return t.state.ChatSubscriptionID != "" && t.state.ChatSubscriptionID == t.request.SubscriptionID
}
func (t *lifecycleTransition) ambiguousUnban() bool {
	if t.request.Action != "unban" {
		return false
	}
	return t.request.ObservedAt.Equal(t.state.ModerationAt) && t.state.ChatRestriction != lifecycle.RestrictionNone
}
func (t *lifecycleTransition) removeBan() (bool, error) {
	s := t.state
	s.ChatRestriction = lifecycle.RestrictionNone
	s.NormalCleanupAt = time.Time{}
	s.NormalCleanupAttempts = 0
	s.TimeoutUntil = time.Time{}
	t.bump()
	t.recover()
	return true, nil
}
func (t *lifecycleTransition) applyRestriction() (bool, error) {
	s, r := t.state, t.request
	if r.Action == "timeout" {
		return t.applyTimeout()
	}
	s.ChatRestriction = lifecycle.RestrictionBanned
	s.TimeoutUntil = time.Time{}
	t.incident("bot_banned")
	t.restrict()
	t.bump()
	return true, nil
}

// A timeout pauses admission without an owner notice or subscription teardown.
func (t *lifecycleTransition) applyTimeout() (bool, error) {
	s, r := t.state, t.request
	if !r.TimeoutUntil.After(r.ObservedAt) {
		return false, errors.New("timeout deadline must follow observation")
	}
	s.ChatRestriction = lifecycle.RestrictionTimeout
	s.TimeoutUntil = r.TimeoutUntil
	t.restrict()
	s.NormalCleanupAt = time.Time{}
	t.bump()
	return true, nil
}

func (t *lifecycleTransition) expireTimeout() (bool, error) {
	s := t.state
	if !t.timeoutExpiryAllowed() {
		return false, nil
	}
	// Expiry permits verification only; it does not itself restore admission.
	s.ChatRestriction = lifecycle.RestrictionNone
	s.TimeoutUntil = time.Time{}
	t.bump()
	t.recover()
	return true, nil
}

func (t *lifecycleTransition) timeoutExpiryAllowed() bool {
	if t.state.ChatRestriction != lifecycle.RestrictionTimeout {
		return false
	}
	if t.now.Before(t.state.TimeoutUntil) {
		return false
	}
	return t.sameGeneration()
}
