// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/valkey"

	"github.com/google/uuid"
	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const roundTTL = 48 * time.Hour

var ErrInvalidTerms = errors.New("draw: invalid terms")

type Config struct {
	Verifier          Verifier
	Payer             Payer
	Announcer         Announcer
	Winners           WinnerSink
	Phases            PhaseObserver
	LegacyKey         func(ChannelID) string
	LegacyDeadlineKey func(ChannelID) string
	Random            func(int64) int64
	Log               *zap.Logger
}

type Store struct {
	client valkey_go.Client
	cfg    Config
	due    valkey.DueIndex
}

func NewStore(client valkey_go.Client, cfg Config) *Store {
	if cfg.Verifier == nil || cfg.Payer == nil {
		panic("draw: verifier and payer are required")
	}
	if cfg.Announcer == nil || cfg.Winners == nil {
		panic("draw: announcer and winner sink are required")
	}
	if cfg.Log == nil {
		cfg.Log = zap.NewNop()
	}
	if cfg.Random == nil {
		cfg.Random = secureRandom
	}
	return &Store{client: valkey.Primary(client), cfg: cfg, due: valkey.DueIndex{Key: dueKey}}
}

func NewRoundID() RoundID { return RoundID(uuid.NewString()) }

type OpenRequest struct {
	ID       RoundID
	Channel  ChannelID
	Terms    Terms
	Duration time.Duration
	Actor    ViewerID
}

type OpenOutcome int64

const (
	Opened       OpenOutcome = OpenOutcome(valkey.RoundOpened)
	JoinableBusy OpenOutcome = OpenOutcome(valkey.RoundJoinableBusy)
	LegacyBusy   OpenOutcome = OpenOutcome(valkey.RoundLegacyBusy)
)

func (s *Store) Open(ctx context.Context, req OpenRequest) (Round, OpenOutcome, error) {
	now, err := valkey.ServerNow(ctx, s.client)
	if err != nil {
		return Round{}, Opened, err
	}
	terms, err := validateTerms(req, now)
	if err != nil {
		return Round{}, Opened, err
	}
	if req.ID == "" {
		req.ID = NewRoundID()
	}
	r := Round{ID: req.ID, Channel: req.Channel, Terms: terms, Phase: PhaseOpen,
		OpenedAt: now.Truncate(time.Millisecond), ClosesAt: terms.ClosesAt}
	if req.Actor != "" {
		r.Actions = []Action{{Kind: "open", Actor: req.Actor, At: r.OpenedAt}}
	}
	spec, err := s.openSpec(r, now.UnixMicro())
	if err != nil {
		return Round{}, Opened, err
	}
	keys := keysFor(r.Channel, r.ID)
	code, err := keys.Open(ctx, s.client, spec)
	if err != nil || OpenOutcome(code) != Opened {
		return Round{}, OpenOutcome(code), err
	}
	return r, Opened, nil
}

func (s *Store) openSpec(r Round, openedOrder int64) (valkey.RoundOpen, error) {
	terms, err := json.Marshal(r.Terms)
	if err != nil {
		return valkey.RoundOpen{}, err
	}
	actions, err := json.Marshal(r.Actions)
	if err != nil {
		return valkey.RoundOpen{}, err
	}
	fields := []valkey.FieldValue{
		{Field: "channel", Value: strconv.FormatUint(uint64(r.Channel), 10)},
		{Field: "kind", Value: string(r.Terms.Kind)}, {Field: "terms", Value: string(terms)},
		{Field: "phase", Value: string(r.Phase)}, timeField("opened_at", r.OpenedAt), timeField("closes_at", r.ClosesAt),
	}
	reminder := r.OpenedAt.Add(Pins.RemindInterval)
	if len(r.Actions) != 0 {
		fields = append(fields, valkey.FieldValue{Field: "actions", Value: string(actions)})
	}
	fields = append(fields, timeField("remind_at", reminder))
	return valkey.RoundOpen{
		RoundID: string(r.ID), Legacy: roundLegacyKey(r.Channel, s.cfg.LegacyKey, legacyStateKey),
		LegacyDeadline: roundLegacyKey(r.Channel, s.cfg.LegacyDeadlineKey, legacyDeadlineKey),
		TTL:            roundTTL, Wake: valkey.DrawWake{Channel: wakeChannel, At: earlier(reminder, r.ClosesAt)}, OpenedMs: openedOrder,
		Fields: fields,
	}, nil
}

func roundLegacyKey(channel ChannelID, key, fallback func(ChannelID) string) string {
	if key != nil {
		return key(channel)
	}
	return fallback(channel)
}

func validateTerms(req OpenRequest, now time.Time) (Terms, error) {
	t := req.Terms
	policy, ok := settlers[t.Kind]
	if !ok || !req.validTarget() {
		return Terms{}, ErrInvalidTerms
	}
	if req.Duration != 0 {
		t.ClosesAt = now.Add(req.Duration)
	}
	t.ClosesAt = t.ClosesAt.Truncate(time.Millisecond)
	if !t.ClosesAt.After(now) || t.ClosesAt.After(now.Add(Pins.MaxOpen)) {
		return Terms{}, ErrInvalidTerms
	}
	if t.ClaimWindow != 0 && !validClaimWindow(t.ClaimWindow) {
		return Terms{}, ErrInvalidTerms
	}
	if t.Winners == (WinnerPolicy{}) {
		t.Winners.Count = policy.defaultWinners
	}
	return t, policy.validate(t)
}

func (r OpenRequest) validTarget() bool {
	return r.Channel != 0 && !strings.ContainsRune(string(r.ID), ':')
}

func validClaimWindow(window time.Duration) bool {
	return window >= Pins.ClaimWindowMin && window <= Pins.ClaimWindowMax
}

type JoinRequest struct {
	Channel ChannelID
	Round   RoundID
	Viewer  ViewerID
	Login   string
	Weight  int64
}

type JoinOutcome = valkey.JoinResult

func (s *Store) Join(ctx context.Context, req JoinRequest) (JoinOutcome, error) {
	if !req.valid() {
		return JoinOutcome{}, errors.New("draw: invalid entrant")
	}
	id := req.Round
	if id == "" {
		var err error
		id, err = s.joinableID(ctx, req.Channel)
		if err != nil || id == "" {
			return JoinOutcome{}, err
		}
	}
	keys := keysFor(req.Channel, id)
	return keys.Join(ctx, s.client, valkey.RoundEntrant{
		Member: string(req.Viewer), Label: req.Login, Weight: req.Weight, TTL: roundTTL,
		CheckDeadline: true, Channel: uint64(req.Channel),
	})
}

func (r JoinRequest) valid() bool {
	return r.Channel != 0 && r.Viewer != "" && r.Weight > 0
}

func (s *Store) joinableID(ctx context.Context, ch ChannelID) (RoundID, error) {
	id, _, err := valkey.ReadString(ctx, s.client, JoinableKey(ch))
	return RoundID(id), err
}

func (s *Store) Joinable(ctx context.Context, ch ChannelID) (Round, bool, error) {
	id, err := s.joinableID(ctx, ch)
	if err != nil || id == "" {
		return Round{}, false, err
	}
	r, err := s.metadata(ctx, ch, id)
	if err != nil || r.Phase != PhaseOpen {
		return Round{}, false, err
	}
	now, err := valkey.ServerNow(ctx, s.client)
	return r, err == nil && now.Before(r.ClosesAt), err
}

func timeField(field string, at time.Time) valkey.FieldValue {
	return valkey.FieldValue{Field: field, Value: strconv.FormatInt(millis(at), 10)}
}
