// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"time"

	"ItsBagelBot/pkg/codec"
)

type ViewerID string
type Kind string
type Phase string
type SlotState string

const (
	KindGiveaway Kind = "giveaway"
	KindRaffle   Kind = "raffle"

	PhaseOpen      Phase = "open"
	PhaseClosing   Phase = "closing"
	PhaseSettling  Phase = "settling"
	PhaseDone      Phase = "done"
	PhaseCancelled Phase = "cancelled"

	SlotPending   SlotState = "pending"
	SlotVerifying SlotState = "verifying"
	SlotWon       SlotState = "won"
	SlotClaimed   SlotState = "claimed"
	SlotNoShow    SlotState = "no_show"
	SlotPaid      SlotState = "paid"
	SlotUnpaid    SlotState = "unpaid"
	SlotUnfilled  SlotState = "unfilled"
	SlotEnded     SlotState = "ended"
)

type Prize struct {
	Item   string
	Points int64
}

type WinnerPolicy struct {
	Count int
	Auto  bool
}

type Terms struct {
	Kind          Kind
	Prize         Prize
	Winners       WinnerPolicy
	ClaimWindow   time.Duration
	RerollNoShows bool
	Rules         codec.RawMessage `json:",omitempty"`
	ClosesAt      time.Time
}

type Round struct {
	ID        RoundID
	Channel   ChannelID
	Terms     Terms
	Phase     Phase
	OpenedAt  time.Time
	ClosesAt  time.Time
	SettledAt time.Time
	Winners   int
	Actions   []Action
}

type Action struct {
	Kind  string
	Actor ViewerID
	At    time.Time
	Value string
}

type Candidate struct {
	Viewer     ViewerID
	Generation int64
	Reason     string
	Detail     map[string]string `json:",omitempty"`
}

type Slot struct {
	N          int         `json:"n"`
	Generation int64       `json:"generation"`
	Viewer     ViewerID    `json:"viewer_id,omitempty"`
	Login      string      `json:"login,omitempty"`
	State      SlotState   `json:"state"`
	Deadline   time.Time   `json:"-"`
	Since      time.Time   `json:"since"`
	Line       int64       `json:"line,omitempty"`
	Instance   int64       `json:"instance,omitempty"`
	History    []Candidate `json:"history,omitempty"`
	ClaimedAt  time.Time   `json:"-"`
	RetryAt    time.Time   `json:"retry_at"`
}

type slotFields Slot

type slotJSON struct {
	slotFields
	Deadline  int64 `json:"deadline,omitempty"`
	ClaimedAt int64 `json:"claimed_at,omitempty"`
}

func (v Slot) MarshalJSON() ([]byte, error) {
	return codec.Marshal(slotJSON{slotFields: slotFields(v), Deadline: millis(v.Deadline), ClaimedAt: millis(v.ClaimedAt)})
}

func (v *Slot) UnmarshalJSON(data []byte) error {
	var wire slotJSON
	if err := codec.Unmarshal(data, &wire); err != nil {
		return err
	}
	*v = Slot(wire.slotFields)
	v.Deadline, v.ClaimedAt = fromMillis(wire.Deadline), fromMillis(wire.ClaimedAt)
	return nil
}

func millis(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

func fromMillis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
