// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package email

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/resend/resend-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingClient struct {
	request  *resend.SendEmailRequest
	key      string
	reply    *resend.SendEmailResponse
	err      error
	calls    int
	deadline time.Time
	bounded  bool
}

func (c *recordingClient) SendWithOptions(ctx context.Context, request *resend.SendEmailRequest, options *resend.SendEmailOptions) (*resend.SendEmailResponse, error) {
	c.request, c.key = request, options.IdempotencyKey
	c.deadline, c.bounded = ctx.Deadline()
	c.calls++
	return c.reply, c.err
}

func validMessage() Message {
	return Message{To: "winner@example.com", Subject: "s", HTML: "<p>h</p>", Text: "t", IdempotencyKey: "key-1"}
}

func newSender(c *recordingClient) *Sender {
	return &Sender{client: c, from: "Bagel <bagel@example.com>"}
}

func TestSendHasDeadline(t *testing.T) {
	client := &recordingClient{reply: &resend.SendEmailResponse{Id: "id-1"}}
	started := time.Now()
	_, err := newSender(client).Send(context.Background(), validMessage())
	require.NoError(t, err)
	require.True(t, client.bounded)
	assert.WithinDuration(t, started.Add(sendTimeout), client.deadline, time.Second)
}

func TestSendMapsMessageToProviderRequest(t *testing.T) {
	client := &recordingClient{reply: &resend.SendEmailResponse{Id: "email-1"}}
	msg := validMessage()
	msg.Headers = map[string]string{"List-Unsubscribe": "<https://example.com/u/t>"}
	msg.Tags = map[string]string{"campaign": "c1"}
	msg.Attachments = []Attachment{{Content: []byte("png"), Filename: "logo.png", ContentType: "image/png", ContentId: "logo"}}

	receipt, err := newSender(client).Send(context.Background(), msg)

	require.NoError(t, err)
	assert.Equal(t, "email-1", receipt.ID)
	assert.Equal(t, "key-1", client.key)
	assert.Equal(t, "Bagel <bagel@example.com>", client.request.From)
	assert.Equal(t, []string{msg.To}, client.request.To)
	assert.Equal(t, msg.Headers, client.request.Headers)
	assert.Equal(t, []resend.Tag{{Name: "campaign", Value: "c1"}}, client.request.Tags)
	require.Len(t, client.request.Attachments, 1)
	assert.Equal(t, "logo", client.request.Attachments[0].ContentId)
}

func TestSendPerMessageSenderOverridesConfig(t *testing.T) {
	client := &recordingClient{reply: &resend.SendEmailResponse{Id: "email-1"}}
	msg := validMessage()
	msg.From = "Original <original@example.com>"
	_, err := newSender(client).Send(context.Background(), msg)
	require.NoError(t, err)
	assert.Equal(t, msg.From, client.request.From)
}

func TestSendFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		client    *recordingClient
		mutate    func(*Message)
		wantErr   error
		wantCalls int
		leaks     []string
	}{
		{
			name:      "hides provider error text",
			client:    &recordingClient{err: errors.New("rejected winner@example.com private-request-detail")},
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
			leaks:     []string{"winner@example.com", "private-request-detail"},
		},
		{
			name:      "classifies a client error as a rejection",
			client:    &recordingClient{err: &resend.ResendError{StatusCode: 422, Message: "winner@example.com"}},
			wantErr:   ErrRejected,
			wantCalls: 1,
			leaks:     []string{"winner@example.com"},
		},
		{
			name:      "keeps a rejection unconfirmed",
			client:    &recordingClient{err: &resend.ResendError{StatusCode: 400}},
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
		},
		{
			name:      "treats a server error as unconfirmed only",
			client:    &recordingClient{err: &resend.ResendError{StatusCode: 503}},
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
		},
		{
			name:      "requires an acceptance id",
			client:    &recordingClient{reply: &resend.SendEmailResponse{}},
			wantErr:   ErrUnconfirmed,
			wantCalls: 1,
		},
		{
			name:    "requires an idempotency key before sending",
			client:  &recordingClient{reply: &resend.SendEmailResponse{Id: "email-1"}},
			mutate:  func(m *Message) { m.IdempotencyKey = "" },
			wantErr: ErrInvalidMessage,
		},
		{
			name:    "rejects an invalid recipient",
			client:  &recordingClient{reply: &resend.SendEmailResponse{Id: "email-1"}},
			mutate:  func(m *Message) { m.To = "not-an-address" },
			wantErr: ErrInvalidMessage,
		},
		{
			name:      "maps provider rate limiting without recipient data",
			client:    &recordingClient{err: &resend.RateLimitError{Message: "recipient data"}},
			wantErr:   ErrRateLimited,
			wantCalls: 1,
			leaks:     []string{"recipient data"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := validMessage()
			if tc.mutate != nil {
				tc.mutate(&msg)
			}
			_, err := newSender(tc.client).Send(context.Background(), msg)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.wantCalls, tc.client.calls)
			for _, leak := range tc.leaks {
				assert.NotContains(t, err.Error(), leak)
			}
		})
	}
}

func TestOnlyClientErrorsAreRejections(t *testing.T) {
	for status, rejected := range map[int]bool{400: true, 403: true, 422: true, 408: false, 409: false, 500: false, 503: false} {
		client := &recordingClient{err: &resend.ResendError{StatusCode: status}}
		_, err := newSender(client).Send(context.Background(), validMessage())
		require.ErrorIs(t, err, ErrUnconfirmed)
		assert.Equal(t, rejected, errors.Is(err, ErrRejected), "status %d", status)
	}
}

func TestSendWithoutSenderAddressIsInvalid(t *testing.T) {
	client := &recordingClient{reply: &resend.SendEmailResponse{Id: "email-1"}}
	_, err := (&Sender{client: client}).Send(context.Background(), validMessage())
	require.ErrorIs(t, err, ErrInvalidMessage)
	assert.Zero(t, client.calls)
}

func TestRetryAfterReadsProviderHeader(t *testing.T) {
	client := &recordingClient{err: &resend.RateLimitError{RetryAfter: "7"}}
	_, err := newSender(client).Send(context.Background(), validMessage())
	wait, ok := RetryAfter(err)
	require.True(t, ok)
	assert.Equal(t, 7*time.Second, wait)

	_, ok = RetryAfter(ErrUnconfirmed)
	assert.False(t, ok)
}

func TestFromEnv(t *testing.T) {
	t.Setenv("RESEND_MARKETING_API", "key")
	t.Setenv("RESEND_MARKETING_FROM", "Bagel <m@example.com>")
	cfg, err := FromEnv("RESEND_MARKETING")
	require.NoError(t, err)
	assert.Equal(t, Config{APIKey: "key", From: "Bagel <m@example.com>"}, cfg)

	_, err = FromEnv("RESEND_UNSET")
	require.ErrorIs(t, err, ErrDisabled)
}
