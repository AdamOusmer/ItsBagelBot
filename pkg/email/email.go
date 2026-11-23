// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package email

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/monitor"

	"github.com/resend/resend-go/v4"
)

const (
	sendTimeout     = 5 * time.Second
	DefaultEndpoint = "https://api.resend.com/emails"
)

var (
	ErrInvalidMessage = errors.New("email is invalid")
	ErrUnconfirmed    = errors.New("email provider acceptance is unconfirmed")
	ErrRateLimited    = errors.New("email provider rate limit reached")
	ErrRejected       = errors.New("email provider rejected the message")
	ErrDisabled       = errors.New("email delivery is not configured")
)

type Config struct {
	APIKey   string
	From     string
	Endpoint string
}

type Attachment struct {
	Content     []byte
	Filename    string
	ContentType string
	ContentId   string
	Path        string
}

type Message struct {
	From           string
	To             string
	Subject        string
	HTML           string
	Text           string
	Headers        map[string]string
	IdempotencyKey string
	Attachments    []Attachment
	Tags           map[string]string
}

type Receipt struct{ ID string }

type client interface {
	SendWithOptions(context.Context, *resend.SendEmailRequest, *resend.SendEmailOptions) (*resend.SendEmailResponse, error)
}

type Sender struct {
	client client
	from   string
}

type rateLimitError struct{ retryAfter time.Duration }

func (rateLimitError) Error() string { return ErrRateLimited.Error() }

func (rateLimitError) Is(target error) bool { return target == ErrRateLimited }

type rejectedError struct{}

func (rejectedError) Error() string { return ErrRejected.Error() }

func (rejectedError) Is(target error) bool { return target == ErrRejected || target == ErrUnconfirmed }

func FromEnv(prefix string) (Config, error) {
	key := env.Get(prefix+"_API", env.Get(prefix+"_API_KEY", ""))
	if key == "" {
		return Config{}, ErrDisabled
	}
	return Config{APIKey: key, From: env.Get(prefix+"_FROM", "")}, nil
}

func New(cfg Config) *Sender {
	api := resend.NewCustomClient(monitor.HTTPClient(sendTimeout), cfg.APIKey)
	if base, err := url.Parse(strings.TrimSuffix(endpointOrDefault(cfg.Endpoint), "emails")); err == nil {
		api.BaseURL = base
	}
	return &Sender{client: api.Emails, from: cfg.From}
}

func endpointOrDefault(endpoint string) string {
	if endpoint == "" {
		return DefaultEndpoint
	}
	return endpoint
}

func RetryAfter(err error) (time.Duration, bool) {
	var limited rateLimitError
	if !errors.As(err, &limited) {
		return 0, false
	}
	return limited.retryAfter, limited.retryAfter > 0
}

func (s *Sender) Send(ctx context.Context, m Message) (Receipt, error) {
	if s == nil || s.client == nil {
		return Receipt{}, ErrInvalidMessage
	}
	from := m.From
	if from == "" {
		from = s.from
	}
	if strings.TrimSpace(from) == "" || !m.valid() {
		return Receipt{}, ErrInvalidMessage
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	reply, err := s.client.SendWithOptions(ctx, m.request(from), &resend.SendEmailOptions{IdempotencyKey: m.IdempotencyKey})
	if err != nil {
		return Receipt{}, safeDeliveryError(err)
	}
	if reply == nil || strings.TrimSpace(reply.Id) == "" {
		return Receipt{}, ErrUnconfirmed
	}
	return Receipt{ID: reply.Id}, nil
}

func (m Message) request(from string) *resend.SendEmailRequest {
	req := &resend.SendEmailRequest{
		From: from, To: []string{m.To}, Subject: m.Subject, Html: m.HTML, Text: m.Text, Headers: m.Headers,
	}
	for _, a := range m.Attachments {
		req.Attachments = append(req.Attachments, &resend.Attachment{
			Content: a.Content, Filename: a.Filename, ContentType: a.ContentType, ContentId: a.ContentId, Path: a.Path,
		})
	}
	for name, value := range m.Tags {
		req.Tags = append(req.Tags, resend.Tag{Name: name, Value: value})
	}
	return req
}

func (m Message) valid() bool {
	for _, field := range []string{m.IdempotencyKey, m.HTML, m.Text} {
		if strings.TrimSpace(field) == "" {
			return false
		}
	}
	return validate.Email(m.To) == nil
}

func classifyProviderError(err error) error {
	var api *resend.ResendError
	if errors.As(err, &api) && rejectedStatus(api.StatusCode) {
		return rejectedError{}
	}
	return ErrUnconfirmed
}

func rejectedStatus(status int) bool {
	if status == http.StatusRequestTimeout || status == http.StatusConflict {
		return false
	}
	return status >= http.StatusBadRequest && status < http.StatusInternalServerError
}

func safeDeliveryError(err error) error {
	var limited *resend.RateLimitError
	if !errors.As(err, &limited) {
		return classifyProviderError(err)
	}
	seconds, _ := strconv.Atoi(strings.TrimSpace(limited.RetryAfter))
	return rateLimitError{retryAfter: time.Duration(seconds) * time.Second}
}
