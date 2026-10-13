package worker

import (
	"ItsBagelBot/internal/channelruntime"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/domain/outgress"
	"context"
	"strconv"
)

func (w *Worker) backgroundAllowed(ctx context.Context, id string) bool {
	if w.runtime == nil {
		return true
	}
	uid, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return false
	}
	s, err := w.runtime.Get(ctx, uid)
	return err == nil && s.AllowsBackground()
}

func (w *Worker) admitPayload(ctx context.Context, p *outgress.Message) (bool, error) {
	if w.runtime == nil {
		return true, nil
	}
	if p.BroadcasterID == "" {
		return true, nil
	}
	// Owner control and recovery reconciliation have their own durable fence.
	if p.Type == outgress.TypeEventSub {
		return true, nil
	}
	uid, err := strconv.ParseUint(p.BroadcasterID, 10, 64)
	if err != nil {
		return false, nil
	}
	snapshot, err := w.runtime.Get(ctx, uid)
	if err != nil {
		return false, err
	}
	if !snapshot.AllowsRuntime() {
		return false, nil
	}
	if !payloadMatchesAdmission(p, snapshot) {
		return false, nil
	}
	p.AccountCreatedAt = snapshot.AccountCreatedAt
	p.AdmissionGeneration = snapshot.State.AdmissionGeneration
	return true, nil
}

func (w *Worker) BackgroundAllowed(ctx context.Context, id string) bool {
	return w.backgroundAllowed(ctx, id)
}

func (w *Worker) enrollmentAllowed(ctx context.Context, e enrollment) bool {
	if w.runtime == nil {
		return true
	}
	uid, err := strconv.ParseUint(e.broadcasterID, 10, 64)
	if err != nil {
		return false
	}
	snapshot, err := w.runtime.Get(ctx, uid)
	if err != nil {
		return false
	}
	if !snapshot.Registered {
		return false
	}
	if snapshot.ServiceBanned {
		return false
	}
	if !enrollmentMatchesAdmission(e, snapshot) {
		return false
	}
	if e.operationID != "" {
		return recoveryEnrollmentAllowed(e, snapshot.State)
	}
	return snapshot.AllowsBackground()
}

func (w *Worker) stampCurrent(ctx context.Context, p *outgress.Message) error {
	if w.runtime == nil {
		return nil
	}
	uid, err := strconv.ParseUint(p.BroadcasterID, 10, 64)
	if err != nil {
		return err
	}
	s, err := w.runtime.Get(ctx, uid)
	if err != nil {
		return err
	}
	p.AccountCreatedAt = s.AccountCreatedAt
	p.AdmissionGeneration = s.State.AdmissionGeneration
	return nil
}

func payloadMatchesAdmission(p *outgress.Message, snapshot channelruntime.Snapshot) bool {
	if p.AccountCreatedAt != 0 && p.AccountCreatedAt != snapshot.AccountCreatedAt {
		return false
	}
	if p.AdmissionGeneration == 0 {
		return snapshot.State.AdmissionGeneration <= 1
	}
	return p.AdmissionGeneration == snapshot.State.AdmissionGeneration
}
func enrollmentMatchesAdmission(e enrollment, snapshot channelruntime.Snapshot) bool {
	if e.incarnation != 0 && e.incarnation != snapshot.AccountCreatedAt {
		return false
	}
	return e.generation == 0 || e.generation == snapshot.State.AdmissionGeneration
}
func recoveryEnrollmentAllowed(e enrollment, state lifecycle.State) bool {
	if state.RuntimeState != lifecycle.Recovering {
		return false
	}
	if state.OperationID != e.operationID {
		return false
	}
	if !state.DesiredEnabled {
		return false
	}
	return state.ChatRestriction == lifecycle.RestrictionNone
}
