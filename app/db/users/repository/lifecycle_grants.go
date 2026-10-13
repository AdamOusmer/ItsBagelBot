package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
)

func (t *lifecycleTransition) loseGrant() (bool, error) {
	s := t.state
	r := t.request
	if !t.freshGrant() || r.ObservedAt.Before(s.GrantObservedAt) {
		return false, nil
	}
	s.GrantObservedAt = r.ObservedAt
	s.GrantEventID = r.EventID
	if s.GrantHealth == lifecycle.GrantMissing {
		return true, nil
	}
	s.GrantHealth = lifecycle.GrantMissing
	t.incident("grant_missing")
	t.restrict()
	t.bump()
	return true, nil
}

func (t *lifecycleTransition) restoreGrant() (bool, error) {
	s := t.state
	r := t.request
	if !t.freshGrant() || !r.ObservedAt.After(s.GrantObservedAt) {
		return false, nil
	}
	s.GrantObservedAt = r.ObservedAt
	s.GrantEventID = r.EventID
	if t.needsControlRepair() {
		s.ControlRepairAt = t.now
		s.ControlRepairAttempts = 0
	}
	if s.GrantHealth == lifecycle.GrantHealthy {
		return true, nil
	}
	s.GrantHealth = lifecycle.GrantHealthy
	t.bump()
	t.recover()
	return true, nil
}

func (t *lifecycleTransition) needsControlRepair() bool {
	if !t.state.DesiredEnabled {
		return false
	}
	return t.state.ChatRestriction != lifecycle.RestrictionNone || !t.state.ControlListenersHealthy
}
