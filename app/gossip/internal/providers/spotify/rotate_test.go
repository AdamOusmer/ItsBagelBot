// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"

	"github.com/stretchr/testify/assert"

	"go.uber.org/zap"
)

// rotatingKeys fakes the full resolver+rotator pair core.SpotifyKeyClient
// implements in production.
type rotatingKeys struct {
	rotateErr error

	calls []rotateCall
}

type rotateCall struct{ broadcaster, prev, next string }

func (f *rotatingKeys) Credentials(context.Context, string) (core.SpotifyCredentials, error) {
	return core.SpotifyCredentials{}, nil
}

func (f *rotatingKeys) Rotate(_ context.Context, broadcaster, prev, next string) error {
	f.calls = append(f.calls, rotateCall{broadcaster, prev, next})
	return f.rotateErr
}

// readOnlyKeys fakes a resolver with no write-back half.
type readOnlyKeys struct{}

func (readOnlyKeys) Credentials(context.Context, string) (core.SpotifyCredentials, error) {
	return core.SpotifyCredentials{}, nil
}

func TestPersistRotationWritesBack(t *testing.T) {
	keys := &rotatingKeys{}
	p := &api{keys: keys, log: zap.NewNop()}

	p.persistRotation(context.Background(), "42", "old-token", "new-token")

	assert.Equal(t, []rotateCall{{"42", "old-token", "new-token"}}, keys.calls)
}

func TestPersistRotationSkipsWithoutRotation(t *testing.T) {
	keys := &rotatingKeys{}
	p := &api{keys: keys, log: zap.NewNop()}

	// No replacement issued, and a replacement identical to the current token:
	// neither is a rotation, neither may reach custody.
	p.persistRotation(context.Background(), "42", "old-token", "")
	p.persistRotation(context.Background(), "42", "old-token", "old-token")

	assert.Empty(t, keys.calls)
}

func TestPersistRotationToleratesWriteBackFailure(t *testing.T) {
	keys := &rotatingKeys{rotateErr: errors.New("custody unreachable")}
	p := &api{keys: keys, log: zap.NewNop()}

	// Must not panic or propagate: the mint already succeeded and the previous
	// token generally stays valid.
	p.persistRotation(context.Background(), "42", "old-token", "new-token")

	assert.Len(t, keys.calls, 1)
}

func TestPersistRotationToleratesReadOnlyResolver(t *testing.T) {
	p := &api{keys: readOnlyKeys{}, log: zap.NewNop()}
	p.persistRotation(context.Background(), "42", "old-token", "new-token")
}
