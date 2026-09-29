// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"

	"github.com/stretchr/testify/assert"

	"go.uber.org/zap"
)

type rotatingKeys struct {
	readOnlyKeys
	rotateErr error

	calls []rotateCall
}

func newTestAPI(keys provider.SpotifyCredResolver) *api {
	return &api{keys: keys, log: zap.NewNop()}
}

type rotateCall struct{ broadcaster, prev, next string }

func (f *rotatingKeys) Rotate(_ context.Context, broadcaster, prev, next string) error {
	f.calls = append(f.calls, rotateCall{broadcaster, prev, next})
	return f.rotateErr
}

type readOnlyKeys struct{}

func (readOnlyKeys) Credentials(context.Context, string) (core.SpotifyCredentials, error) {
	return core.SpotifyCredentials{}, nil
}

func TestPersistRotationWritesBack(t *testing.T) {
	keys := &rotatingKeys{}
	p := newTestAPI(keys)

	p.persistRotation(context.Background(), "42", "old-token", "new-token")

	assert.Equal(t, []rotateCall{{"42", "old-token", "new-token"}}, keys.calls)
}

func TestPersistRotationSkipsWithoutRotation(t *testing.T) {
	keys := &rotatingKeys{}
	p := newTestAPI(keys)

	p.persistRotation(context.Background(), "42", "old-token", "")
	p.persistRotation(context.Background(), "42", "old-token", "old-token")

	assert.Empty(t, keys.calls)
}

func TestPersistRotationToleratesWriteBackFailure(t *testing.T) {
	keys := &rotatingKeys{rotateErr: errors.New("custody unreachable")}
	p := newTestAPI(keys)

	p.persistRotation(context.Background(), "42", "old-token", "new-token")

	assert.Len(t, keys.calls, 1)
}

func TestPersistRotationToleratesReadOnlyResolver(t *testing.T) {
	p := newTestAPI(readOnlyKeys{})
	p.persistRotation(context.Background(), "42", "old-token", "new-token")
}

type deadMarkingKeys struct {
	readOnlyKeys
	calls [][2]string
}

func (f *deadMarkingKeys) MarkDead(_ context.Context, broadcaster, token string) error {
	f.calls = append(f.calls, [2]string{broadcaster, token})
	return nil
}

func TestReportRefreshFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want [][2]string
	}{
		{"revoked grant", &core.UpstreamError{Status: http.StatusBadRequest, Message: "invalid_grant"}, [][2]string{{"42", "rt"}}},
		{"bad client", &core.UpstreamError{Status: http.StatusBadRequest, Message: "invalid_client"}, nil},
		{"outage", &core.UpstreamError{Status: http.StatusServiceUnavailable}, nil},
		{"transport", errors.New("dial"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keys := &deadMarkingKeys{}
			p := newTestAPI(keys)
			p.reportRefreshFailure(context.Background(), "42", "rt", tc.err)
			assert.Equal(t, tc.want, keys.calls)
		})
	}
}
