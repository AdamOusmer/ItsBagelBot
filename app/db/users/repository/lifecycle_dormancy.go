package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"time"
)

func (t *lifecycleTransition) engage() (bool, error) {
	s := t.state
	r := t.request
	if !t.freshActivity() {
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
		t.bump()
	}
	return true, nil
}

func (t *lifecycleTransition) claimDormancy() (bool, error) {
	s := t.state

	if !t.dormancyClaimAllowed() {
		return false, nil
	}
	s.DormancyClaimUntil = t.now.Add(time.Minute)
	s.DormancyAfter = s.DormancyClaimUntil
	return true, nil
}

func (t *lifecycleTransition) probeDormancy() (bool, error) {
	s := t.state
	if !t.dormancyProbeAllowed() {
		return false, nil
	}
	s.DormancyProbeAt = t.now
	return true, nil
}

func (t *lifecycleTransition) postponeLiveDormancy() (bool, error) {
	s := t.state

	if !t.activeGeneration() {
		return false, nil
	}
	s.DormancyAfter = t.now.Add(time.Hour)
	s.DormancyClaimUntil = time.Time{}
	s.DormancyUnknownSince = time.Time{}
	return true, nil
}

func (t *lifecycleTransition) deferDormancy() (bool, error) {
	s := t.state

	if !t.activeGeneration() {
		return false, nil
	}
	if s.DormancyUnknownSince.IsZero() {
		s.DormancyUnknownSince = t.now
	}
	s.DormancyAfter = t.now.Add(time.Hour)
	deadline := s.DormancyUnknownSince.Add(24 * time.Hour)
	if deadline.Before(s.DormancyAfter) {
		s.DormancyAfter = deadline
	}
	if !s.DormancyAfter.After(t.now) {
		s.DormancyAfter = t.now.Add(time.Minute)
	}
	s.DormancyClaimUntil = time.Time{}
	return true, nil
}

func (t *lifecycleTransition) enterDormancy() (bool, error) {
	s := t.state
	if !t.dormancyThresholdPassed() {
		return false, nil
	}
	if !t.dormancyEvidenceAllows() {
		return false, nil
	}
	s.RuntimeState = lifecycle.Dormant
	s.DormancyAfter = time.Time{}
	s.DormancyClaimUntil = time.Time{}
	t.bump()
	return true, nil
}

func (t *lifecycleTransition) freshActivity() bool {
	r, s := t.request, t.state
	if !s.AllowsRuntime() {
		return false
	}
	if r.EventID == "" {
		return false
	}
	if r.EventID == s.LastActivityID {
		return false
	}
	if r.ObservedAt.IsZero() {
		return false
	}
	return r.ObservedAt.After(s.LastEngagedAt)
}
func (t *lifecycleTransition) dormancyClaimAllowed() bool {
	if !t.dormancyThresholdPassed() {
		return false
	}
	if t.now.Before(t.state.DormancyAfter) {
		return false
	}
	return !t.now.Before(t.state.DormancyClaimUntil)
}
func (t *lifecycleTransition) dormancyThresholdPassed() bool {
	return t.activeGeneration() && !t.now.Before(t.state.LastEngagedAt.Add(lifecycle.DormancyPeriod))
}
func (t *lifecycleTransition) dormancyProbeAllowed() bool {
	if !t.activeGeneration() {
		return false
	}
	if t.state.DormancyProbeAt.IsZero() {
		return true
	}
	return t.now.Sub(t.state.DormancyProbeAt) >= 24*time.Hour
}
func (t *lifecycleTransition) dormancyEvidenceAllows() bool {
	if t.request.Reason == "offline" {
		return true
	}
	if t.request.Reason != "unavailable" {
		return false
	}
	if t.state.DormancyUnknownSince.IsZero() {
		return false
	}
	return !t.now.Before(t.state.DormancyUnknownSince.Add(24 * time.Hour))
}
