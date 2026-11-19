// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"errors"
)

type Verifier interface {
	Verify(context.Context, Check) (Verdict, error)
}

type Check struct {
	Round  Round
	Viewer ViewerID
	Login  string
}

type Verdict struct {
	Pass   bool
	Reason string
}

type PassAll struct{}

func (PassAll) Verify(context.Context, Check) (Verdict, error) { return Verdict{Pass: true}, nil }

type PaymentKey string

type Payment struct {
	Channel  ChannelID
	Viewer   ViewerID
	Login    string
	Points   int64
	Key      PaymentKey
	Instance int64
}

var ErrPaymentRejected = errors.New("draw: payment rejected")

type Payer interface {
	Pay(context.Context, Payment) error
	Instance(context.Context, ChannelID, ViewerID) (int64, error)
}

type rejectPayment struct{}

func (rejectPayment) Pay(context.Context, Payment) error { return ErrPaymentRejected }
func (rejectPayment) Instance(context.Context, ChannelID, ViewerID) (int64, error) {
	return 0, nil
}

type NoticeEvent string

const (
	NoticeWinners   NoticeEvent = "winners"
	NoticeVerifying NoticeEvent = "verifying"
	NoticeNoShow    NoticeEvent = "no_show"
	NoticeEmpty     NoticeEvent = "empty"
	NoticeUnfilled  NoticeEvent = "unfilled"
)

type Notice struct {
	Event NoticeEvent
	Round Round
	Slots []Slot
}

type Announcer interface {
	Announce(context.Context, Notice)
}

type nopAnnouncer struct{}

func (nopAnnouncer) Announce(context.Context, Notice) {}

type WinnerSink interface {
	Record(context.Context, Round, []Slot) error
}

type NopSink struct{}

func (NopSink) Record(context.Context, Round, []Slot) error { return nil }
