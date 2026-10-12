package repository

import (
	"ItsBagelBot/internal/domain/lifecycle"
	"time"
)

func (t *lifecycleTransition) acknowledgeNotice() (bool, error) {
	s := t.state
	if !t.noticeAcknowledgmentAllowed() {
		return false, nil
	}
	s.IncidentAnnounced = true
	if s.RetractionIncidentID == "" {
		s.NoticeNextAttemptAt = time.Time{}
	}
	return true, nil
}

func (t *lifecycleTransition) claimNotice() (bool, error) {
	s := t.state

	if !t.noticeClaimAllowed() {
		return false, nil
	}
	t.expireUndeliveredNotices()
	s.NoticeNextAttemptAt = t.now.Add(time.Minute)
	if t.noticeWorkComplete() {
		s.NoticeNextAttemptAt = time.Time{}
	}
	return true, nil
}

func (t *lifecycleTransition) expireUndeliveredNotices() {
	s := t.state
	if s.IncidentID != "" && !s.IncidentAnnounced && lifecycleTimeDue(s.NoticeDeadline, t.now) {
		s.IncidentAnnounced = true
	}
	if s.RetractionIncidentID != "" && lifecycleTimeDue(s.RetractionExpiresAt, t.now) {
		t.popRetraction()
	}
}

func (t *lifecycleTransition) acknowledgeRetraction() (bool, error) {
	r := t.request
	if r.IncidentID == "" || r.IncidentID != t.state.RetractionIncidentID {
		return false, nil
	}
	t.popRetraction()
	if t.noticeWorkComplete() {
		t.state.NoticeNextAttemptAt = time.Time{}
	}
	return true, nil
}

func (t *lifecycleTransition) popRetraction() {
	s := t.state
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
		s.NoticeNextAttemptAt = t.now
	}
}

func (t *lifecycleTransition) noticeAcknowledgmentAllowed() bool {
	if t.request.IncidentID == "" {
		return false
	}
	if t.request.IncidentID != t.state.IncidentID {
		return false
	}
	return !t.state.IncidentAnnounced
}
func (t *lifecycleTransition) noticeClaimAllowed() bool {
	if t.now.Before(t.state.NoticeNextAttemptAt) {
		return false
	}
	return t.state.IncidentID != "" || t.state.RetractionIncidentID != ""
}
func (t *lifecycleTransition) noticeWorkComplete() bool {
	if t.state.RetractionIncidentID != "" {
		return false
	}
	return t.state.IncidentID == "" || t.state.IncidentAnnounced
}
