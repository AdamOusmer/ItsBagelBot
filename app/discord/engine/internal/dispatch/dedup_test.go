// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/discord/engine/internal/dispatch"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

type countMetrics struct{ dup, failOpen int }

func (m *countMetrics) Duplicate() { m.dup++ }
func (m *countMetrics) FailOpen()  { m.failOpen++ }

func deliver(t *testing.T, handle func(*bus.Message) error, id string) {
	t.Helper()
	raw, err := codec.Marshal(memberPayload(testGuild))
	require.NoError(t, err)
	body, err := codec.Marshal(ddiscord.Event{Type: "GUILD_MEMBER_ADD", GuildID: testGuild, Raw: raw})
	require.NoError(t, err)
	require.NoError(t, handle(bus.NewMessage(id, body)))
}

func TestEventDedup(t *testing.T) {
	cfg := ddiscord.Config{GuildID: testGuild, WelcomeChannelID: testWelcomeCh}
	cases := []struct {
		name       string
		store      *seenStore
		ids        []string
		wantEmbeds int
		wantDup    int
		wantOpen   int
	}{
		{"same id twice runs handlers once", &seenStore{claimed: map[string]bool{}}, []string{"s-1", "s-1"}, 1, 1, 0},
		{"different ids both run", &seenStore{claimed: map[string]bool{}}, []string{"s-1", "s-2"}, 2, 0, 0},
		{"empty id always runs", &seenStore{claimed: map[string]bool{}}, []string{"", ""}, 2, 0, 0},
		{"store error fails open", &seenStore{claimed: map[string]bool{}, err: errors.New("valkey down")}, []string{"s-1", "s-1"}, 2, 0, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, cfg)
			metrics := &countMetrics{}
			handle := dispatch.Dedup(idempotency.NewTiered(16, tc.store), zap.NewNop(), metrics)(h.d.Handle)

			for _, id := range tc.ids {
				deliver(t, handle, id)
			}

			require.Len(t, h.log.byType(ddiscord.TypePostEmbed), tc.wantEmbeds)
			require.Equal(t, tc.wantDup, metrics.dup)
			require.Equal(t, tc.wantOpen, metrics.failOpen)
		})
	}
}
