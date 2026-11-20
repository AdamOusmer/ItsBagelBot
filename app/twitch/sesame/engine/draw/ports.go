// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"context"
	"errors"
	"time"
)

type Verifier interface {
	Verify(context.Context, Check) (Verdict, error)
}

type Check struct {
	Round  Round
	Viewer ViewerID
	Login  string
	Entry  Entry
}

type Entry struct {
	Weight int64
}

type Verdict struct {
	Pass   bool
	Reason string
	Detail map[string]string
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

type NoticeEvent string

const (
	NoticeWinners   NoticeEvent = "winners"
	NoticeVerifying NoticeEvent = "verifying"
	NoticeNoShow    NoticeEvent = "no_show"
	NoticeEmpty     NoticeEvent = "empty"
	NoticeUnpaid    NoticeEvent = "unpaid"
	NoticeRemind    NoticeEvent = "remind"
	NoticeUnfilled  NoticeEvent = "unfilled"
)

type Notice struct {
	Event    NoticeEvent
	Round    Round
	Slots    []Slot
	Entries  int
	TimeLeft time.Duration
}

type Announcer interface {
	Announce(context.Context, Notice)
}

type WinnerSink interface {
	Record(context.Context, Round, []Slot) error
}

type DiscardWinnerRecords struct{}

func (DiscardWinnerRecords) Record(context.Context, Round, []Slot) error { return nil }

type PhaseObserver interface {
	LeftOpen(context.Context, Round)
}
