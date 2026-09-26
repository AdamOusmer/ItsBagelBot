// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func chattersTestClient(transport roundTripFunc) *Client {
	c := NewClient("client", &Source{token: "app-token", expires: time.Now().Add(time.Hour)}, &Source{token: "bot-token", expires: time.Now().Add(time.Hour)}, nil)
	c.SetTransport(transport)
	return c
}
func chatterResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestChattersPageBindsTenantAndBotToken(t *testing.T) {
	broadcaster := "123"
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if req.URL.Path != chattersPath || q.Get("broadcaster_id") != broadcaster || q.Get("moderator_id") != "456" || q.Get("first") != "1000" || q.Get("after") != "opaque &cursor" {
			t.Fatalf("unexpected request: %s", req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer bot-token" || req.Header.Get("Client-Id") != "client" {
			t.Fatal("incorrect token identity")
		}
		return chatterResponse(200, fmt.Sprintf(`{"data":[{"user_id":"%s","user_login":"alice"}],"pagination":{"cursor":"next"}}`, broadcaster)), nil
	})
	for _, id := range []string{"123", "789"} {
		broadcaster = id
		page, err := c.GetChattersPage(t.Context(), id, "456", "opaque &cursor")
		if err != nil || page.Complete || page.NextCursor != "next" || len(page.Chatters) != 1 || page.Chatters[0].ID != id {
			t.Fatalf("tenant page mismatch: %+v %v", page, err)
		}
	}
}

func TestChattersContinuesBeyondThirtyPages(t *testing.T) {
	calls := 0
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) {
		calls++
		cursor := req.URL.Query().Get("after")
		page := 1
		if cursor != "" {
			n, err := strconv.Atoi(cursor)
			if err != nil {
				t.Fatal(err)
			}
			page = n + 1
		}
		next := fmt.Sprintf(`{"cursor":"%d"}`, page)
		if page == 35 {
			next = "{}"
		}
		return chatterResponse(200, fmt.Sprintf(`{"data":[{"user_id":"%d","user_login":"viewer"}],"pagination":%s}`, page, next)), nil
	})
	cursor := ""
	for i := 1; i <= 35; i++ {
		page, err := c.GetChattersPage(t.Context(), "123", "456", cursor)
		if err != nil {
			t.Fatal(err)
		}
		if page.Complete != (i == 35) {
			t.Fatalf("page %d completeness=%v", i, page.Complete)
		}
		cursor = page.NextCursor
	}
	if calls != 35 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestChattersConvenienceListingReportsTruncation(t *testing.T) {
	calls := 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return chatterResponse(200, fmt.Sprintf(`{"data":[{"user_id":"%d"}],"pagination":{"cursor":"%d"}}`, calls, calls)), nil
	})
	list, err := c.GetChatters(t.Context(), "123", "456")
	if !errors.Is(err, ErrChattersIncomplete) || len(list) != 30 || calls != 30 {
		t.Fatalf("silent truncation: count=%d calls=%d err=%v", len(list), calls, err)
	}
}

func TestChattersRepeatedCursorRefused(t *testing.T) {
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		return chatterResponse(200, `{"data":[],"pagination":{"cursor":"same"}}`), nil
	})
	_, err := c.GetChattersPage(t.Context(), "123", "456", "same")
	if !errors.Is(err, ErrRepeatedCursor) {
		t.Fatalf("err=%v", err)
	}
}

func TestChattersChargesEveryAttemptIncluding401Retry(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return chatterResponse(401, `{"message":"invalid token"}`), nil
		}
		return chatterResponse(200, `{"data":[],"pagination":{}}`), nil
	})
	c.user.refresh = func(context.Context) (string, time.Duration, error) { return "renewed", time.Hour, nil }
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error { admitted++; return nil })
	_, err := c.GetChattersPage(ctx, "123", "456", "")
	if err != nil || calls != 2 || admitted != 2 {
		t.Fatalf("calls=%d admission=%d err=%v", calls, admitted, err)
	}
}

func TestChattersRetryAdmissionDenialPreventsSecondHTTP(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return chatterResponse(401, `{"message":"invalid token"}`), nil
	})
	c.user.refresh = func(context.Context) (string, time.Duration, error) { return "renewed", time.Hour, nil }
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error {
		admitted++
		if admitted == 2 {
			return &AdmissionError{Code: "rate_limited", RetryAt: time.Now().Add(time.Second)}
		}
		return nil
	})
	_, err := c.GetChattersPage(ctx, "123", "456", "")
	var denied *AdmissionError
	if !errors.As(err, &denied) || calls != 1 || admitted != 2 {
		t.Fatalf("calls=%d admission=%d err=%v", calls, admitted, err)
	}
}

func TestChatters429PreservesProviderReset(t *testing.T) {
	reset := time.Now().Add(time.Minute).Truncate(time.Second)
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		r := chatterResponse(429, `{}`)
		r.Header.Set("Ratelimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		return r, nil
	})
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error { return nil })
	_, err := c.GetChattersPage(ctx, "123", "456", "")
	var denied *AdmissionError
	if !errors.As(err, &denied) || denied.Code != "rate_limited" || denied.RetryAt.Sub(reset) > time.Millisecond || reset.Sub(denied.RetryAt) > time.Millisecond {
		t.Fatalf("reset lost: err=%v", err)
	}
}

func TestChattersExpiredContextDoesNotSpendOrCallHTTP(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return chatterResponse(200, `{"data":[],"pagination":{}}`), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx = WithAttemptAdmission(ctx, func(context.Context, string) error { admitted++; return nil })
	_, err := c.GetChattersPage(ctx, "123", "456", "")
	if !errors.Is(err, context.Canceled) || calls != 0 || admitted != 0 {
		t.Fatalf("expired context executed: calls=%d admission=%d err=%v", calls, admitted, err)
	}
}

func TestChattersFreshLiveReadUsesAppTokenAndAdmission(t *testing.T) {
	admitted := 0
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/helix/streams" || req.URL.Query().Get("user_id") != "123" || req.Header.Get("Authorization") != "Bearer app-token" {
			t.Fatalf("wrong live identity: %v", req.URL)
		}
		return chatterResponse(200, `{"data":[{"type":"live"}]}`), nil
	})
	ctx := WithAttemptAdmission(t.Context(), func(_ context.Context, endpoint string) error {
		admitted++
		if endpoint != "/helix/streams?user_id=123" {
			t.Fatalf("wrong admission route %q", endpoint)
		}
		return nil
	})
	live, err := c.IsStreamLive(ctx, "123")
	if err != nil || !live || admitted != 1 {
		t.Fatalf("live=%v admission=%d err=%v", live, admitted, err)
	}
}

func TestChattersRejectsMalformedSuccessEnvelope(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":[]}`, `{"pagination":{}}`, `{"data":null,"pagination":{}}`, `{"data":[],"pagination":null}`, `{"data":{},"pagination":{}}`, `{"data":[],"pagination":[]}`} {
		t.Run(body, func(t *testing.T) {
			c := chattersTestClient(func(*http.Request) (*http.Response, error) { return chatterResponse(200, body), nil })
			if page, err := c.GetChattersPage(t.Context(), "123", "456", ""); err == nil {
				t.Fatalf("malformed success became complete attendance: %+v", page)
			}
		})
	}
}

func TestReadyCredentialsRevalidateAdmissionBeforeHTTP(t *testing.T) {
	for _, code := range []string{"inactive", "rate_limited"} {
		t.Run(code, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			source := &Source{refresh: func(context.Context) (string, time.Duration, error) {
				close(entered)
				<-release
				return "bot-token", time.Hour, nil
			}}
			c := NewClient("client", NewStaticTokenSource("app"), source, nil)
			calls := 0
			admissions := 0
			var revoked atomic.Bool
			c.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return chatterResponse(200, `{"data":[],"pagination":{}}`), nil
			}))
			ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error {
				admissions++
				if revoked.Load() {
					return &AdmissionError{Code: code, RetryAt: time.Now().Add(time.Minute)}
				}
				return nil
			})
			done := make(chan error)
			go func() { _, err := c.GetChattersPage(ctx, "123", "456", ""); done <- err }()
			<-entered
			revoked.Store(true)
			close(release)
			err := <-done
			var denied *AdmissionError
			if !errors.As(err, &denied) || denied.Code != code || calls != 0 || admissions != 1 {
				t.Fatalf("stale precredential admission reached HTTP: %v calls=%d admission=%d", err, calls, admissions)
			}
		})
	}
}

func TestTokenSingleflightWaitRespectsCallerDeadline(t *testing.T) {
	source := &Source{}
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		source.group.Do("refresh-0", func() (any, error) { close(entered); <-release; return refreshResult{token: "valid"}, nil })
		close(done)
	}()
	<-entered
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { _, err := source.Token(ctx); returned <- err }()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected token result %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("caller deadline blocked behind shared refresh")
	}
}

func TestStreamSessionReturnsProviderIdentityAndValidatesBinding(t *testing.T) {
	for _, tc := range []struct {
		body    string
		invalid bool
	}{
		{`{"data":[{"id":"stream-42","user_id":"123","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`, false},
		{`{"data":[{"id":"stream-42","user_id":"789","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`, true},
		{`{"data":[{"user_id":"123","type":"live","started_at":"2026-09-26T00:00:00Z"}]}`, true},
		{`{"data":[{"id":"stream-42","user_id":"123","type":"live"}]}`, true},
		{`{}`, true},
		{`{"data":null}`, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			c := chattersTestClient(func(*http.Request) (*http.Response, error) { return chatterResponse(200, tc.body), nil })
			id, started, live, err := c.StreamSession(t.Context(), "123")
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid provider session accepted")
				}
				return
			}
			if err != nil || !live || id != "stream-42" || started.IsZero() {
				t.Fatalf("lost stream identity: %s %v %v %v", id, started, live, err)
			}
		})
	}
}
