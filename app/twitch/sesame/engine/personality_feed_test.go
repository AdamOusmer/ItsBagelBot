// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package engine

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

type failingFeedPersister struct {
	calls int
	id    string
	err   error
}

func (p *failingFeedPersister) FeedBump(_ context.Context, _ uint64, _, id string) (FeedTotals, error) {
	p.calls++
	p.id = id
	return FeedTotals{}, p.err
}
func (*failingFeedPersister) FeedBoard(context.Context, uint64, int) (FeedBoard, error) {
	return FeedBoard{}, nil
}

func TestFeedDatabaseFailurePrecedesCacheMutation(t *testing.T) {
	failure := errors.New("database unavailable")
	backend := &failingFeedPersister{err: failure}
	// A nil cache panics if the feeding touches Valkey before the durable write.
	store := NewValkeyPersonality(nil, backend, nil)
	_, err := store.Feed(context.Background(), 77, "Crumb", "chat-one")
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, backend.calls)
	firstID := backend.id
	require.Len(t, firstID, 64)
	_, err = store.Feed(context.Background(), 77, "Crumb renamed", "chat-one")
	require.ErrorIs(t, err, failure)
	require.Equal(t, firstID, backend.id)
	_, err = store.Feed(context.Background(), 78, "Crumb", "chat-one")
	require.ErrorIs(t, err, failure)
	require.NotEqual(t, firstID, backend.id)
}

func TestFeedRequiresReplayIdentity(t *testing.T) {
	backend := &failingFeedPersister{}
	store := NewValkeyPersonality(nil, backend, nil)
	_, err := store.Feed(context.Background(), 77, "Crumb", "")
	require.Error(t, err)
	require.Zero(t, backend.calls)
}
