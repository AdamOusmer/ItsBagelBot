package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"errors"
	"time"
)

func (t *lifecycleTransition) acknowledgeSeed() (bool, error) {

	return true, nil
}

func (t *lifecycleTransition) seed() (bool, error) {

	return true, nil
}

func (t *lifecycleTransition) seedBlocked() (bool, error) {
	s := t.state
	r := t.request
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
	// Legacy blocking cleared is_active; only an enabled registry entry is seeded here.
	s.DesiredEnabled = true
	t.incident(r.Reason)
	s.IncidentAnnounced = true
	s.NoticeNextAttemptAt = time.Time{}
	t.restrict()
	t.bump()
	return true, nil
}

func (t *lifecycleTransition) enable() (bool, error) {
	s := t.state
	if s.DesiredEnabled && s.RuntimeState == lifecycle.Active {
		return false, nil
	}
	s.DesiredEnabled = true
	t.bump()
	if s.ChatRestriction != lifecycle.RestrictionNone || s.GrantHealth == lifecycle.GrantMissing {
		t.restrict()
	} else {
		t.recover()
	}
	return true, nil
}

func (t *lifecycleTransition) disable() (bool, error) {
	s := t.state
	s.NormalCleanupAt = t.now
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
	t.bump()
	return true, nil
}
