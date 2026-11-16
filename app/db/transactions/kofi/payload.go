// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kofi

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
)

type Kind string

const (
	KindDonation     Kind = "donation"
	KindSubscription Kind = "subscription"
	KindCommission   Kind = "commission"
	KindShopOrder    Kind = "shop_order"
)

var Kinds = []Kind{KindDonation, KindSubscription, KindCommission, KindShopOrder}

var kindByType = map[string]Kind{
	"Tip":          KindDonation,
	"Donation":     KindDonation,
	"Subscription": KindSubscription,
	"Commission":   KindCommission,
	"Shop Order":   KindShopOrder,
}

type Token string

var (
	ErrToken       = errors.New("ko-fi verification token rejected")
	ErrMalformed   = errors.New("ko-fi data is not a json object")
	ErrPayload     = errors.New("ko-fi payload invalid")
	ErrUnsupported = errors.New("ko-fi payment type unsupported")
)

var (
	amountPattern     = regexp.MustCompile(`^([0-9]{1,12})(?:\.([0-9]{1,2}))?$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

const (
	maxTierRunes = 100
	// Ko-fi's settings-page test webhook carries this placeholder id.
	testTransactionID = "00000000-1111-2222-3333-444444444444"
)

type Donation struct {
	TransactionID     string
	MessageID         string
	Kind              Kind
	AmountCents       int64
	Currency          string
	Subscription      bool
	FirstSubscription bool
	TierName          string
	OccurredAt        time.Time
}

type webhookData struct {
	VerificationToken          string  `json:"verification_token"`
	MessageID                  string  `json:"message_id"`
	Timestamp                  string  `json:"timestamp"`
	Type                       string  `json:"type"`
	Amount                     string  `json:"amount"`
	Currency                   string  `json:"currency"`
	IsSubscriptionPayment      bool    `json:"is_subscription_payment"`
	IsFirstSubscriptionPayment bool    `json:"is_first_subscription_payment"`
	TransactionID              string  `json:"kofi_transaction_id"`
	TierName                   *string `json:"tier_name"`
}

func (d Donation) Test() bool {
	return d.TransactionID == testTransactionID
}

func Parse(data []byte, token Token) (Donation, error) {
	var raw webhookData
	if codec.Unmarshal(data, &raw) != nil {
		return Donation{}, ErrMalformed
	}
	if !token.matches(raw.VerificationToken) {
		return Donation{}, ErrToken
	}
	return raw.donation()
}

func (t Token) matches(provided string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(provided)) == 1
}

type check struct {
	field string
	ok    bool
}

func firstInvalid(checks ...check) error {
	for _, c := range checks {
		if !c.ok {
			return fmt.Errorf("%w: %s", ErrPayload, c.field)
		}
	}
	return nil
}

func (raw webhookData) donation() (Donation, error) {
	kind, known := kindByType[raw.Type]
	if !known {
		return Donation{}, ErrUnsupported
	}
	cents, amountOK := raw.cents()
	occurred, timeErr := time.Parse(time.RFC3339, raw.Timestamp)
	currency := strings.ToUpper(strings.TrimSpace(raw.Currency))
	if err := firstInvalid(
		check{field: "kofi_transaction_id", ok: identifierPattern.MatchString(raw.TransactionID)},
		check{field: "message_id", ok: identifierPattern.MatchString(raw.MessageID)},
		check{field: "amount", ok: amountOK},
		check{field: "timestamp", ok: timeErr == nil},
		check{field: "currency", ok: currencyPattern.MatchString(currency)},
	); err != nil {
		return Donation{}, err
	}
	return Donation{
		TransactionID:     raw.TransactionID,
		MessageID:         raw.MessageID,
		Kind:              kind,
		AmountCents:       cents,
		Currency:          currency,
		Subscription:      raw.IsSubscriptionPayment,
		FirstSubscription: raw.IsFirstSubscriptionPayment,
		TierName:          raw.tier(),
		OccurredAt:        occurred.UTC(),
	}, nil
}

func (raw webhookData) cents() (int64, bool) {
	parts := amountPattern.FindStringSubmatch(strings.TrimSpace(raw.Amount))
	if parts == nil {
		return 0, false
	}
	units, _ := strconv.ParseInt(parts[1], 10, 64)
	fraction, _ := strconv.ParseInt((parts[2] + "00")[:2], 10, 64)
	return units*100 + fraction, true
}

func (raw webhookData) tier() string {
	if raw.TierName == nil {
		return ""
	}
	runes := []rune(strings.TrimSpace(*raw.TierName))
	return string(runes[:min(len(runes), maxTierRunes)])
}
