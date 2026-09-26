// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// AdmissionError carries scheduling information without exposing provider
// response bodies. Rate denials never reach HTTP.
type AdmissionError struct {
	// Provider distinguishes an observed HTTP 429 from a local quota denial.
	Provider bool
	// ProviderObserved means the header-time observer persisted this reset.
	ProviderObserved bool
	Code             string
	RetryAt          time.Time
}

func (e *AdmissionError) Error() string { return "Twitch request admission: " + e.Code }

type attemptAdmissionKey struct{}
type AttemptAdmission func(context.Context, string) error

// WithAttemptAdmission applies to every HTTP attempt within ctx, including
// the retry after a 401. Other client consumers keep their existing admission.
func WithAttemptAdmission(ctx context.Context, admit AttemptAdmission) context.Context {
	return context.WithValue(ctx, attemptAdmissionKey{}, admit)
}
func admitAttempt(ctx context.Context, endpoint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if admit, ok := ctx.Value(attemptAdmissionKey{}).(AttemptAdmission); ok && admit != nil {
		return admit(ctx, endpoint)
	}
	return nil
}
func observeAttempt(ctx context.Context, res *http.Response) error {
	_, admitted := ctx.Value(attemptAdmissionKey{}).(AttemptAdmission)
	_, observed := ctx.Value(providerResetObserverKey{}).(ProviderResetObserver)
	if (!admitted && !observed) || res.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	reset := time.Now().Add(time.Second)
	if seconds, err := strconv.ParseInt(res.Header.Get("Ratelimit-Reset"), 10, 64); err == nil && seconds > 0 {
		reset = time.Unix(seconds, 0)
	}
	return &AdmissionError{Provider: true, Code: "rate_limited", RetryAt: reset}
}

// ProviderResetObserver receives a real HTTP429 at headers, before body cleanup.
// It is context scoped, so other Client consumers retain their own policy.
type ProviderResetObserver func(context.Context, string, time.Time) error
type providerResetObserverKey struct{}

func WithProviderResetObserver(ctx context.Context, observe ProviderResetObserver) context.Context {
	return context.WithValue(ctx, providerResetObserverKey{}, observe)
}
func publishProviderReset(ctx context.Context, endpoint string, err error) error {
	observed, ok := err.(*AdmissionError)
	if !ok || !observed.Provider {
		return err
	}
	if observe, ok := ctx.Value(providerResetObserverKey{}).(ProviderResetObserver); ok && observe != nil {
		if observerErr := observe(ctx, endpoint, observed.RetryAt); observerErr != nil {
			return observerErr
		}
		observed.ProviderObserved = true
	}
	return observed
}
