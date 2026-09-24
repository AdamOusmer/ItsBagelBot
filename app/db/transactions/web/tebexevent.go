// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"time"

	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
	"ItsBagelBot/pkg/codec"
)

type tebexEvent struct {
	ID      string           `json:"id"`
	Type    string           `json:"type"`
	Date    string           `json:"date"`
	Subject codec.RawMessage `json:"subject"`
}

type paymentSubject struct {
	TransactionID             string                      `json:"transaction_id"`
	RecurringPaymentReference string                      `json:"recurring_payment_reference"`
	Custom                    map[string]codec.RawMessage `json:"custom"`
	Customer                  struct {
		Username usernameRef `json:"username"`
	} `json:"customer"`
	Products []productLine `json:"products"`
}

type productLine struct {
	Custom    map[string]codec.RawMessage `json:"custom"`
	Username  usernameRef                 `json:"username"`
	ExpiresAt string                      `json:"expires_at"`
}

type recurringSubject struct {
	Reference      string          `json:"reference"`
	NextPaymentAt  string          `json:"next_payment_at"`
	InitialPayment *paymentSubject `json:"initial_payment"`
	LastPayment    *paymentSubject `json:"last_payment"`
}

type usernameRef struct {
	ID codec.RawMessage `json:"id"`
}

type recordablePayment struct {
	TransactionID      string
	UserID             uint64
	GiftedByID         uint64
	GiftedByLogin      string
	GiftMessage        string
	RecurringReference string
	ExpiresAt          *time.Time
}

var (
	errNoRecordablePayment = errors.New("no recordable Tebex payment for event")
	errPaymentUserMissing  = errors.New("payment user id missing")
)

var billingEventActions = map[string]struct {
	action billingrpc.Action
	notify bool
}{
	"payment.completed":                        {billingrpc.ActionActivate, true},
	"recurring-payment.started":                {billingrpc.ActionActivate, true},
	"recurring-payment.renewed":                {billingrpc.ActionActivate, true},
	"recurring-payment.cancellation.requested": {billingrpc.ActionCancelRequested, false},
	"recurring-payment.cancellation.aborted":   {billingrpc.ActionCancelAborted, false},
	"payment.dispute.won":                      {billingrpc.ActionCancelAborted, false},
	"recurring-payment.ended":                  {billingrpc.ActionRevoke, false},
	"payment.refunded":                         {billingrpc.ActionRevoke, false},
	"payment.dispute.opened":                   {billingrpc.ActionRevoke, false},
	"payment.dispute.lost":                     {billingrpc.ActionRevoke, false},
}

func grantsPaid(action billingrpc.Action) bool {
	return action == billingrpc.ActionActivate ||
		action == billingrpc.ActionCancelRequested ||
		action == billingrpc.ActionCancelAborted
}

func trialAction(eventType string) (billingrpc.Action, bool) {
	switch {
	case strings.Contains(eventType, "cancel"):
		return billingrpc.ActionCancelRequested, true
	case strings.Contains(eventType, "start"):
		return billingrpc.ActionActivate, true
	default:
		return "", false
	}
}

func recordableFromEvent(event tebexEvent) (recordablePayment, error) {
	switch {
	case strings.HasPrefix(event.Type, "payment."):
		var payment paymentSubject
		if err := codec.Unmarshal(event.Subject, &payment); err != nil {
			return recordablePayment{}, err
		}
		return recordableFromPayment(payment)

	case strings.HasPrefix(event.Type, "recurring-payment."):
		return recordableFromRecurring(event.Type, event.Subject)

	default:
		return recordablePayment{}, errNoRecordablePayment
	}
}

func recordableFromRecurring(eventType string, subject codec.RawMessage) (recordablePayment, error) {
	var recurring recurringSubject
	if err := codec.Unmarshal(subject, &recurring); err != nil {
		return recordablePayment{}, err
	}
	source, ok := pickRecurringPayment(recurring, eventType)
	if !ok {
		return recordablePayment{}, errNoRecordablePayment
	}
	payment, err := recordableFromPayment(*source)
	if err != nil {
		return recordablePayment{}, err
	}
	payment.RecurringReference = recurring.Reference
	if expiresAt, ok := parseTebexTime(recurring.NextPaymentAt); ok {
		payment.ExpiresAt = &expiresAt
	}
	return payment, nil
}

func pickRecurringPayment(recurring recurringSubject, eventType string) (*paymentSubject, bool) {
	if eventType == "recurring-payment.renewed" && recurring.LastPayment != nil {
		return recurring.LastPayment, true
	}
	if recurring.InitialPayment != nil {
		return recurring.InitialPayment, true
	}
	if recurring.LastPayment != nil {
		return recurring.LastPayment, true
	}
	return nil, false
}

func recordableFromPayment(payment paymentSubject) (recordablePayment, error) {

	if payment.TransactionID == "" {
		return recordablePayment{}, errors.New("payment transaction_id missing")
	}
	userID, ok := userIDFromPayment(payment)
	if !ok {
		return recordablePayment{TransactionID: payment.TransactionID}, errPaymentUserMissing
	}

	giftedBy, giftedByLogin := giftFromPayment(payment)
	giftMessage := giftMessageFromPayment(payment)
	if giftedBy == userID {
		giftedBy, giftedByLogin, giftMessage = 0, "", ""
	}
	return recordablePayment{
		TransactionID:      payment.TransactionID,
		UserID:             userID,
		GiftedByID:         giftedBy,
		GiftedByLogin:      giftedByLogin,
		GiftMessage:        giftMessage,
		RecurringReference: payment.RecurringPaymentReference,
		ExpiresAt:          latestProductExpiry(payment.Products),
	}, nil
}

func latestProductExpiry(products []productLine) *time.Time {
	var latest *time.Time
	for _, product := range products {
		expiresAt, ok := parseTebexTime(product.ExpiresAt)
		if !ok {
			continue
		}
		if latest == nil || expiresAt.After(*latest) {
			e := expiresAt
			latest = &e
		}
	}
	return latest
}

func parseTebexTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	return parsed, err == nil
}

func giftFromPayment(payment paymentSubject) (uint64, string) {

	if id, ok := rawUint(payment.Custom["gifted_by"]); ok {
		return id, rawString(payment.Custom["gifted_by_login"])
	}
	for _, product := range payment.Products {
		if id, ok := rawUint(product.Custom["gifted_by"]); ok {
			return id, rawString(product.Custom["gifted_by_login"])
		}
	}
	return 0, ""
}

func giftMessageFromPayment(payment paymentSubject) string {

	if msg := rawString(payment.Custom["gift_message"]); msg != "" {
		return msg
	}
	for _, product := range payment.Products {
		if msg := rawString(product.Custom["gift_message"]); msg != "" {
			return msg
		}
	}
	return ""
}

func rawString(raw codec.RawMessage) string {
	var value string
	if err := codec.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func userIDFromPayment(payment paymentSubject) (uint64, bool) {

	if userID, ok := userIDFromCustom(payment.Custom); ok {
		return userID, true
	}
	if userID, ok := rawUint(payment.Customer.Username.ID); ok {
		return userID, true
	}

	for _, product := range payment.Products {
		if userID, ok := userIDFromCustom(product.Custom); ok {
			return userID, true
		}
		if userID, ok := rawUint(product.Username.ID); ok {
			return userID, true
		}
	}

	return 0, false
}

func userIDFromCustom(custom map[string]codec.RawMessage) (uint64, bool) {

	for _, key := range []string{"user_id", "twitch_user_id", "broadcaster_user_id"} {
		if userID, ok := rawUint(custom[key]); ok {
			return userID, true
		}
	}
	return 0, false
}

func rawUint(raw codec.RawMessage) (uint64, bool) {
	raw = unquoteJSON(bytes.TrimSpace(raw))
	if len(raw) == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(string(raw), 10, 64)
	return parsed, err == nil && parsed != 0
}

func unquoteJSON(raw []byte) []byte {
	if len(raw) < 2 {
		return raw
	}
	if raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}
