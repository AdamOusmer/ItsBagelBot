package repository

import (
	"ItsBagelBot/app/db/users/ent"
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
	switch req.Action {
	case "erase_claim", "erase_ack":
		return r.lifecycleErase(ctx, req)
	case "due":
		return r.lifecycleDue(ctx)
	}
	for attempt := 0; attempt < 8; attempt++ {
		reply, retry, err := r.lifecycleAttempt(ctx, req)
		if !retry {
			return reply, err
		}
	}
	return usersrpc.LifecycleReply{}, errors.New("concurrent lifecycle mutation; retry")
}

type lifecycleMutation struct {
	row     *ent.User
	state   lifecycle.State
	request usersrpc.LifecycleRequest
	now     time.Time
}

func newLifecycleMutation(row *ent.User, req usersrpc.LifecycleRequest) lifecycleMutation {
	now := time.Now().UTC()
	state := LegacyLifecycle(row, now)
	if legacyEngagementSeed(row, req) {
		state.LastEngagedAt = req.ObservedAt.Add(-time.Nanosecond)
	}
	return lifecycleMutation{row: row, state: state, request: req, now: now}
}
func legacyEngagementSeed(row *ent.User, req usersrpc.LifecycleRequest) bool {
	if row.Lifecycle != nil {
		return false
	}
	if req.Action != "engage" {
		return false
	}
	return !req.ObservedAt.IsZero()
}
func (m lifecycleMutation) reply() usersrpc.LifecycleReply {
	return usersrpc.LifecycleReply{State: m.state, AccountCreatedAt: m.row.CreatedAt.UnixMicro(), StateRevision: m.row.StateRevision}
}
func (m lifecycleMutation) readRequest() (usersrpc.LifecycleReply, bool, error) {
	reply := m.reply()
	req := m.request
	if req.AccountCreatedAt != 0 && req.AccountCreatedAt != reply.AccountCreatedAt {
		return reply, true, nil
	}
	switch req.Action {
	case "get":
		return reply, true, nil
	case "notice_eligible":
		reply.Applied = m.noticeEligible()
		return reply, true, nil
	}
	if m.missingIncarnation() {
		return reply, true, errors.New("lifecycle observation requires account incarnation")
	}
	return reply, false, nil
}
func (m lifecycleMutation) missingIncarnation() bool {
	if m.request.Action == "enable" || m.request.Action == "disable" {
		return false
	}
	return m.request.AccountCreatedAt == 0
}
func (m lifecycleMutation) noticeEligible() bool {
	if m.request.AccountCreatedAt != m.row.CreatedAt.UnixMicro() {
		return false
	}
	if m.request.IncidentID == "" {
		return false
	}
	if m.request.IncidentID != m.state.IncidentID {
		return false
	}
	if m.state.IncidentAnnounced {
		return false
	}
	return m.state.DesiredEnabled
}
func (r *Users) lifecycleAttempt(ctx context.Context, req usersrpc.LifecycleRequest) (usersrpc.LifecycleReply, bool, error) {
	row, err := r.client.User.Get(ctx, req.UserID)
	if err != nil {
		return usersrpc.LifecycleReply{}, false, err
	}
	m := newLifecycleMutation(row, req)
	if reply, handled, err := m.readRequest(); handled {
		return reply, false, err
	}
	reply := m.reply()
	changed, err := transitionLifecycle(&m.state, req, m.now, row.Banned)
	if err != nil || !changed {
		return reply, false, err
	}
	committed, err := r.commitLifecycle(ctx, m)
	if err != nil {
		return reply, false, err
	}
	if !committed {
		return reply, true, nil
	}
	reply, err = r.publishCommittedLifecycle(ctx, m)
	return reply, false, err
}
