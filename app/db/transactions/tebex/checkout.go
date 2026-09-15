// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
)

const CheckoutBaseURL = "https://checkout.tebex.io"

var (
	ErrMutationDisabled = errors.New("tebex subscription mutation is disabled until provider behavior is validated")
	ErrCheckoutResponse = errors.New("tebex checkout response is incomplete or invalid")
	ErrCheckoutUnknown  = errors.New("tebex checkout request outcome is unknown")
	ErrUnsafeProtection = errors.New("subscription protection requires review")
)

type CheckoutConfig struct {
	ProjectID       string
	PrivateKey      string
	BaseURL         string
	HTTPClient      *http.Client
	EnableMutations bool
	MinimumNotice   time.Duration
}

type CheckoutClient struct{ cfg CheckoutConfig }

type PauseRequest struct {
	Status      string    `json:"status"`
	PausedUntil time.Time `json:"paused_until"`
}

// Reactivation belongs to Tebex's verified paused_until behavior. The worker
// has no Active mutation that could override a customer's cancellation.
type RecurringProvider interface {
	GetRecurring(context.Context, string) (RecurringPayment, error)
	PauseRecurring(context.Context, string, time.Time) (RecurringPayment, error)
}

type checkoutOperation struct {
	Reference string
	Pause     *PauseRequest
}

func NewCheckoutClient(cfg CheckoutConfig) (*CheckoutClient, error) {
	if strings.TrimSpace(cfg.ProjectID) == "" || strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, errors.New("tebex checkout project id and private key required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = CheckoutBaseURL
	}
	if cfg.MinimumNotice <= 0 {
		cfg.MinimumNotice = 72 * time.Hour
	}
	cfg.HTTPClient = checkoutHTTPClient(cfg.HTTPClient)
	return &CheckoutClient{cfg: cfg}, nil
}

func checkoutHTTPClient(source *http.Client) *http.Client {
	client := http.Client{Timeout: 10 * time.Second}
	if source != nil {
		client = *source
	}
	if client.Timeout <= 0 {
		client.Timeout = 10 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

func (c *CheckoutClient) GetRecurring(ctx context.Context, reference string) (RecurringPayment, error) {
	return c.execute(ctx, checkoutOperation{Reference: reference})
}

func (c *CheckoutClient) PauseRecurring(ctx context.Context, reference string, until time.Time) (RecurringPayment, error) {
	if !c.cfg.EnableMutations {
		return RecurringPayment{}, ErrMutationDisabled
	}
	if until.IsZero() {
		return RecurringPayment{}, ErrUnsafeProtection
	}
	before, err := c.GetRecurring(ctx, reference)
	if err != nil {
		return RecurringPayment{}, err
	}
	if before.ProtectedThrough(reference, until) {
		return before, nil
	}
	if !c.canPause(before, reference, until) {
		return RecurringPayment{}, ErrUnsafeProtection
	}
	return c.pauseAndVerify(ctx, checkoutOperation{Reference: reference, Pause: &PauseRequest{Status: "Paused", PausedUntil: until.UTC()}})
}

func (c *CheckoutClient) pauseAndVerify(ctx context.Context, op checkoutOperation) (RecurringPayment, error) {
	// Always read after a mutation, even if its reply was lost. Its target is
	// absolute; this method never adds another duration on retry.
	_, mutationErr := c.execute(ctx, op)
	after, readErr := c.GetRecurring(ctx, op.Reference)
	if readErr == nil && after.ProtectedThrough(op.Reference, op.Pause.PausedUntil) {
		return after, nil
	}
	if mutationErr != nil || readErr != nil {
		return RecurringPayment{}, ErrCheckoutUnknown
	}
	return after, ErrUnsafeProtection
}

func (c *CheckoutClient) canPause(payment RecurringPayment, reference string, until time.Time) bool {
	if !payment.CanProtect(reference) || until.IsZero() {
		return false
	}
	boundary := payment.NextPaymentDate
	if payment.Status == "Paused" {
		boundary = payment.PausedUntil
	}
	if boundary == nil || boundary.Before(time.Now().Add(c.cfg.MinimumNotice)) {
		return false
	}
	return until.After(*boundary)
}

func (c *CheckoutClient) execute(ctx context.Context, op checkoutOperation) (RecurringPayment, error) {
	req, err := c.request(ctx, op)
	if err != nil {
		return RecurringPayment{}, err
	}
	response, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return RecurringPayment{}, ErrCheckoutUnknown
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return RecurringPayment{}, fmt.Errorf("tebex checkout responded %d", response.StatusCode)
	}
	return decodeRecurring(response.Body, op.Reference)
}

func (c *CheckoutClient) request(ctx context.Context, op checkoutOperation) (*http.Request, error) {
	if !validReference(op.Reference) {
		return nil, ErrUnsafeProtection
	}
	method, path := http.MethodGet, "/api/recurring-payments/"+url.PathEscape(op.Reference)
	var body io.Reader
	if op.Pause != nil {
		data, err := codec.Marshal(op.Pause)
		if err != nil {
			return nil, ErrUnsafeProtection
		}
		method, path, body = http.MethodPut, path+"/status", bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.BaseURL, "/")+path, body)
	if err != nil {
		return nil, ErrUnsafeProtection
	}
	req.SetBasicAuth(c.cfg.ProjectID, c.cfg.PrivateKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func validReference(reference string) bool {
	if reference == "" {
		return false
	}
	for _, char := range reference {
		if !referenceCharacter(char) {
			return false
		}
	}
	return true
}

func referenceCharacter(char rune) bool {
	return strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", char)
}

func decodeRecurring(body io.Reader, reference string) (RecurringPayment, error) {
	const maxResponse = 1 << 20
	data, err := io.ReadAll(io.LimitReader(body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return RecurringPayment{}, ErrCheckoutResponse
	}
	var payment RecurringPayment
	if codec.Unmarshal(data, &payment) != nil || !payment.MatchesReference(reference) {
		return RecurringPayment{}, ErrCheckoutResponse
	}
	return payment, nil
}
