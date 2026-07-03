package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	billingrpc "ItsBagelBot/internal/domain/rpc/billing"
)

// This file is the Tebex-event domain: the wire shapes, the classification of an
// event type into a billing action, and the parsing of a verified event into the
// recordablePayment the rest of the service acts on. It holds no HTTP or storage
// concern.

type tebexEvent struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Date    string          `json:"date"`
	Subject json.RawMessage `json:"subject"`
}

type paymentSubject struct {
	TransactionID             string                     `json:"transaction_id"`
	RecurringPaymentReference string                     `json:"recurring_payment_reference"`
	Custom                    map[string]json.RawMessage `json:"custom"`
	Customer                  struct {
		Username usernameRef `json:"username"`
	} `json:"customer"`
	Products []struct {
		Custom    map[string]json.RawMessage `json:"custom"`
		Username  usernameRef                `json:"username"`
		ExpiresAt string                     `json:"expires_at"`
	} `json:"products"`
}

type recurringSubject struct {
	Reference      string          `json:"reference"`
	NextPaymentAt  string          `json:"next_payment_at"`
	InitialPayment *paymentSubject `json:"initial_payment"`
	LastPayment    *paymentSubject `json:"last_payment"`
}

type usernameRef struct {
	ID json.RawMessage `json:"id"`
}

type recordablePayment struct {
	TransactionID string
	UserID        uint64
	// Gift attribution from the basket custom payload; zero for self-purchases.
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

// billingEventActions maps each Tebex event type that changes an entitlement to
// the action it applies. notify is true for the activation group, where a
// first-time gift warrants a recipient ping (notifyGift itself skips renewals).
// Handling a new event type is one entry here, not a new branch.
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

// trialAction picks the billing action a trial event drives, or false when the
// event (ending-soon reminder, trial ended) changes nothing.
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
		if err := json.Unmarshal(event.Subject, &payment); err != nil {
			return recordablePayment{}, err
		}
		return recordableFromPayment(payment)

	case strings.HasPrefix(event.Type, "recurring-payment."):
		var recurring recurringSubject
		if err := json.Unmarshal(event.Subject, &recurring); err != nil {
			return recordablePayment{}, err
		}
		var payment recordablePayment
		var err error
		if event.Type == "recurring-payment.renewed" && recurring.LastPayment != nil {
			payment, err = recordableFromPayment(*recurring.LastPayment)
		} else if recurring.InitialPayment != nil {
			payment, err = recordableFromPayment(*recurring.InitialPayment)
		} else if recurring.LastPayment != nil {
			payment, err = recordableFromPayment(*recurring.LastPayment)
		} else {
			return recordablePayment{}, errNoRecordablePayment
		}
		if err != nil {
			return recordablePayment{}, err
		}
		payment.RecurringReference = recurring.Reference
		if expiresAt, ok := parseTebexTime(recurring.NextPaymentAt); ok {
			payment.ExpiresAt = &expiresAt
		}
		return payment, nil

	default:
		return recordablePayment{}, errNoRecordablePayment
	}
}

func recordableFromPayment(payment paymentSubject) (recordablePayment, error) {

	if payment.TransactionID == "" {
		return recordablePayment{}, errors.New("payment transaction_id missing")
	}

	if userID, ok := userIDFromPayment(payment); ok {
		giftedBy, giftedByLogin := giftFromPayment(payment)
		giftMessage := giftMessageFromPayment(payment)
		// A basket gifted to yourself is a plain purchase; drop the markers.
		if giftedBy == userID {
			giftedBy, giftedByLogin, giftMessage = 0, "", ""
		}
		result := recordablePayment{
			TransactionID:      payment.TransactionID,
			UserID:             userID,
			GiftedByID:         giftedBy,
			GiftedByLogin:      giftedByLogin,
			GiftMessage:        giftMessage,
			RecurringReference: payment.RecurringPaymentReference,
		}
		for _, product := range payment.Products {
			if expiresAt, ok := parseTebexTime(product.ExpiresAt); ok && (result.ExpiresAt == nil || expiresAt.After(*result.ExpiresAt)) {
				result.ExpiresAt = &expiresAt
			}
		}
		return result, nil
	}

	return recordablePayment{TransactionID: payment.TransactionID}, errPaymentUserMissing
}

func parseTebexTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	return parsed, err == nil
}

// giftFromPayment reads the gifted_by attribution the basket carried. Checked
// on the payment-level custom payload first, then per-product (mirrors
// userIDFromPayment's search order).
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

// giftMessageFromPayment reads the buyer's optional gift note the basket
// carried. Same search order as giftFromPayment: payment-level custom first,
// then per-product.
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

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
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

func userIDFromCustom(custom map[string]json.RawMessage) (uint64, bool) {

	for _, key := range []string{"user_id", "twitch_user_id", "broadcaster_user_id"} {
		if userID, ok := rawUint(custom[key]); ok {
			return userID, true
		}
	}
	return 0, false
}

// rawUint reads a uint64 id from a raw JSON value that may be a number (123) or
// a string ("123") — ids ride the custom payload as strings. It parses with
// strconv directly on the trimmed bytes, avoiding the per-call json.Decoder and
// bytes.Reader the naive path allocated; the surrounding quotes of a JSON string
// id (never escaped for digits) are stripped in place.
func rawUint(raw json.RawMessage) (uint64, bool) {
	raw = bytes.TrimSpace(raw)
	if n := len(raw); n >= 2 && raw[0] == '"' && raw[n-1] == '"' {
		raw = raw[1 : n-1]
	}
	if len(raw) == 0 {
		return 0, false
	}
	parsed, err := strconv.ParseUint(string(raw), 10, 64)
	return parsed, err == nil && parsed != 0
}
