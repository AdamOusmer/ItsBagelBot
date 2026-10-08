// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"errors"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

type seenStore struct {
	claimed map[string]bool
	err     error
}

func (s *seenStore) Seen(_ context.Context, key string, _ time.Duration) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	dup := s.claimed[key]
	s.claimed[key] = true
	return dup, nil
}

func (s *seenStore) Release(context.Context, string) error { return nil }

func deliver(t *testing.T, h *harness, id string) {
	t.Helper()
	raw, err := codec.Marshal(memberPayload(testGuild))
	require.NoError(t, err)
	body, err := codec.Marshal(ddiscord.Event{Type: "GUILD_MEMBER_ADD", GuildID: testGuild, Raw: raw})
	require.NoError(t, err)
	require.NoError(t, h.d.Handle(bus.NewMessage(id, body)))
}

func TestEventDedup(t *testing.T) {
	cfg := ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh}
	cases := []struct {
		name       string
		store      *seenStore
		ids        []string
		wantEmbeds int
	}{
		{"same id twice runs handlers once", &seenStore{claimed: map[string]bool{}}, []string{"s-1", "s-1"}, 1},
		{"different ids both run", &seenStore{claimed: map[string]bool{}}, []string{"s-1", "s-2"}, 2},
		{"empty id always runs", &seenStore{claimed: map[string]bool{}}, []string{"", ""}, 2},
		{"store error fails open", &seenStore{claimed: map[string]bool{}, err: errors.New("valkey down")}, []string{"s-1", "s-1"}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, cfg)
			h.d.Dedup = tc.store

			for _, id := range tc.ids {
				deliver(t, h, id)
			}

			require.Len(t, h.log.byType(ddiscord.TypePostEmbed), tc.wantEmbeds)
		})
	}
}
