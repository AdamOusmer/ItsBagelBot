// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func chattersTestClient(transport roundTripFunc) *Client {
	c := NewClient("client", &Source{token: "app-token", expires: time.Now().Add(time.Hour)}, &Source{token: "bot-token", expires: time.Now().Add(time.Hour)}, nil)
	c.SetTransport(transport)
	return c
}

func TestChattersPageBindsTenantAndBotToken(t *testing.T) {
	broadcaster := "123"
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		require.Equal(t, chattersPath, req.URL.Path)
		require.Equal(t, broadcaster, q.Get("broadcaster_id"))
		require.Equal(t, "456", q.Get("moderator_id"))
		require.Equal(t, "1000", q.Get("first"))
		require.Equal(t, "opaque &cursor", q.Get("after"))
		require.Equal(t, "Bearer bot-token", req.Header.Get("Authorization"))
		require.Equal(t, "client", req.Header.Get("Client-Id"))

		return respond(200, fmt.Sprintf(`{"data":[{"user_id":"%s","user_login":"alice"}],"pagination":{"cursor":"next"}}`, broadcaster)), nil
	})
	for _, id := range []string{"123", "789"} {
		broadcaster = id
		page, err := c.GetChattersPage(t.Context(), ChattersPageRequest{BroadcasterID: id, ModeratorID: "456", Cursor: "opaque &cursor"})
		require.NoError(t, err)
		require.False(t, page.Complete)
		require.Equal(t, "next", page.NextCursor)
		require.Equal(t, 1, len(page.Chatters))
		require.Equal(t, id, page.Chatters[0].ID)
	}
}

// numberedChatterPage models a provider cursor independently of the caller's
// persisted progress, so pagination tests cover continuation rather than mocks.
func numberedChatterPage(t *testing.T, req *http.Request, total int) *http.Response {
	t.Helper()
	page := 1
	if cursor := req.URL.Query().Get("after"); cursor != "" {
		n, err := strconv.Atoi(cursor)
		require.NoError(t, err)
		page = n + 1
	}
	next := fmt.Sprintf(`{"cursor":"%d"}`, page)
	if page == total {
		next = "{}"
	}
	return respond(200, fmt.Sprintf(`{"data":[{"user_id":"%d","user_login":"viewer"}],"pagination":%s}`, page, next))
}
func TestChattersContinuesBeyondThirtyPages(t *testing.T) {
	calls := 0
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) { calls++; return numberedChatterPage(t, req, 35), nil })
	cursor := ""
	for i := 1; i <= 35; i++ {
		page, err := c.GetChattersPage(t.Context(), ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: cursor})
		require.NoError(t, err)
		require.Equal(t, i == 35, page.Complete, "page %d", i)
		cursor = page.NextCursor
	}
	require.Equal(t, 35, calls)
}

func TestChattersConvenienceListingReportsTruncation(t *testing.T) {
	calls := 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return respond(200, fmt.Sprintf(`{"data":[{"user_id":"%d"}],"pagination":{"cursor":"%d"}}`, calls, calls)), nil
	})
	list, err := c.GetChatters(t.Context(), "123", "456")
	require.True(t, errors.Is(err, ErrChattersIncomplete))
	require.Equal(t, 30, len(list))
	require.Equal(t, 30, calls)
}

func TestChattersRepeatedCursorRefused(t *testing.T) {
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		return respond(200, `{"data":[],"pagination":{"cursor":"same"}}`), nil
	})
	_, err := c.GetChattersPage(t.Context(), ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: "same"})
	require.True(t, errors.Is(err, ErrRepeatedCursor))
}

func TestChattersChargesEveryAttemptIncluding401Retry(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return respond(401, `{"message":"invalid token"}`), nil
		}
		return respond(200, `{"data":[],"pagination":{}}`), nil
	})
	c.user.refresh = func(context.Context) (string, time.Duration, error) { return "renewed", time.Hour, nil }
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error { admitted++; return nil })
	_, err := c.GetChattersPage(ctx, ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, 2, admitted)
}

func TestChattersRetryAdmissionDenialPreventsSecondHTTP(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return respond(401, `{"message":"invalid token"}`), nil
	})
	c.user.refresh = func(context.Context) (string, time.Duration, error) { return "renewed", time.Hour, nil }
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error {
		admitted++
		if admitted == 2 {
			return &AdmissionError{Code: "rate_limited", RetryAt: time.Now().Add(time.Second)}
		}
		return nil
	})
	_, err := c.GetChattersPage(ctx, ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
	var denied *AdmissionError
	require.True(t, errors.As(err, &denied))
	require.Equal(t, 1, calls)
	require.Equal(t, 2, admitted)
}

func TestChatters429PreservesProviderReset(t *testing.T) {
	reset := time.Now().Add(time.Minute).Truncate(time.Second)
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		r := respond(429, `{}`)
		r.Header.Set("Ratelimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		return r, nil
	})
	ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error { return nil })
	_, err := c.GetChattersPage(ctx, ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
	var denied *AdmissionError
	require.True(t, errors.As(err, &denied))
	require.Equal(t, "rate_limited", denied.Code)
	require.LessOrEqual(t, denied.RetryAt.Sub(reset), time.Millisecond)
	require.LessOrEqual(t, reset.Sub(denied.RetryAt), time.Millisecond)
}

func TestChattersExpiredContextDoesNotSpendOrCallHTTP(t *testing.T) {
	calls, admitted := 0, 0
	c := chattersTestClient(func(*http.Request) (*http.Response, error) {
		calls++
		return respond(200, `{"data":[],"pagination":{}}`), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ctx = WithAttemptAdmission(ctx, func(context.Context, string) error { admitted++; return nil })
	_, err := c.GetChattersPage(ctx, ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
	require.True(t, errors.Is(err, context.Canceled))
	require.Equal(t, 0, calls)
	require.Equal(t, 0, admitted)
}

func TestChattersFreshLiveReadUsesAppTokenAndAdmission(t *testing.T) {
	admitted := 0
	c := chattersTestClient(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/helix/streams", req.URL.Path)
		require.Equal(t, "123", req.URL.Query().Get("user_id"))
		require.Equal(t, "Bearer app-token", req.Header.Get("Authorization"))

		return respond(200, `{"data":[{"type":"live"}]}`), nil
	})
	ctx := WithAttemptAdmission(t.Context(), func(_ context.Context, endpoint string) error {
		admitted++
		require.Equal(t, "/helix/streams?user_id=123", endpoint)

		return nil
	})
	live, err := c.IsStreamLive(ctx, "123")
	require.NoError(t, err)
	require.True(t, live)
	require.Equal(t, 1, admitted)
}

func TestChattersRejectsMalformedSuccessEnvelope(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":[]}`, `{"pagination":{}}`, `{"data":null,"pagination":{}}`, `{"data":[],"pagination":null}`, `{"data":{},"pagination":{}}`, `{"data":[],"pagination":[]}`} {
		t.Run(body, func(t *testing.T) {
			c := chattersTestClient(func(*http.Request) (*http.Response, error) { return respond(200, body), nil })
			{
				_, err := c.GetChattersPage(t.Context(), ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
				require.Error(t, err)
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
				return respond(200, `{"data":[],"pagination":{}}`), nil
			}))
			ctx := WithAttemptAdmission(t.Context(), func(context.Context, string) error {
				admissions++
				if revoked.Load() {
					return &AdmissionError{Code: code, RetryAt: time.Now().Add(time.Minute)}
				}
				return nil
			})
			done := make(chan error)
			go func() {
				_, err := c.GetChattersPage(ctx, ChattersPageRequest{BroadcasterID: "123", ModeratorID: "456", Cursor: ""})
				done <- err
			}()
			<-entered
			revoked.Store(true)
			close(release)
			err := <-done
			var denied *AdmissionError
			require.True(t, errors.As(err, &denied))
			require.Equal(t, code, denied.Code)
			require.Equal(t, 0, calls)
			require.Equal(t, 1, admissions)
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
			c := chattersTestClient(func(*http.Request) (*http.Response, error) { return respond(200, tc.body), nil })
			id, started, live, err := c.StreamSession(t.Context(), "123")
			if tc.invalid {
				require.NotEqual(t, nil, err)

				return
			}
			require.NoError(t, err)
			require.True(t, live)
			require.Equal(t, "stream-42", id)
			require.False(t, started.IsZero())
		})
	}
}

func TestChattersAuthorizationReasons(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		status               int
		body, scope, message string
	}{
		{"missing scope", 401, `{"message":"Missing scope: moderator:read:chatters"}`, "moderator:read:chatters", "bot token missing moderator:read:chatters; reauthorize the bot in the admin console"},
		{"not moderator", 403, `{"message":"moderator forbidden"}`, "", "bot does not have moderator access to this channel"},
		{"unexpected body", 401, `{"message":"unexpected-secret-provider-content"}`, "", "bot token is not authorized to read chatters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeChattersPage(respond(tc.status, tc.body))
			var failure *ChatterAuthorizationError
			require.ErrorAs(t, err, &failure)
			require.ErrorIs(t, err, ErrMissingScope)
			require.Equal(t, tc.scope, failure.MissingScope)
			require.EqualError(t, err, tc.message)
		})
	}
}

func TestChattersAdoptsReauthorizedBotTokenAfterMissingScope(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	stored := StoredLoad{AccessToken: "under-scoped", AccessTokenExpiresAt: &expiry}
	source := NewStoredUserTokenSource(ClientCredentials{}, "", StoredTokenIO{
		Load: func(context.Context) StoredLoad { return stored },
		Persist: func(context.Context, string, string, time.Time) error {
			t.Fatal("adopting a reauthorized token must not rotate or persist it")
			return nil
		},
	}, MintLease{})
	requests := 0
	client := NewClient("client", nil, source, nil)
	client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Header.Get("Authorization") == "Bearer under-scoped" {
			return respond(401, `{"message":"Missing scope: moderator:read:chatters"}`), nil
		}
		require.Equal(t, "Bearer reauthorized", req.Header.Get("Authorization"))
		return respond(200, `{"data":[{"user_id":"viewer","user_login":"viewer"}],"pagination":{}}`), nil
	}))
	request := ChattersPageRequest{BroadcasterID: "channel", ModeratorID: "bot"}
	_, err := client.GetChattersPage(t.Context(), request)
	require.ErrorIs(t, err, ErrMissingScope)
	require.Equal(t, 1, requests, "a missing scope must not trigger a pointless refresh")
	stored.AccessToken = "reauthorized"
	page, err := client.GetChattersPage(t.Context(), request)
	require.NoError(t, err)
	require.True(t, page.Complete)
	require.Len(t, page.Chatters, 1)
	require.Equal(t, 2, requests)
}
