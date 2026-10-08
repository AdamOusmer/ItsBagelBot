// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func fleetStreamSpecs() []StreamSpec {
	specs := append([]StreamSpec{}, DataStreams...)
	return append(specs, OutgressStream, OutgressSystemStream, YouTubeOutgressStream, YouTubeIngressStream)
}

func ingressStreamSpec(t *testing.T) StreamSpec {
	t.Helper()
	for i := range DataStreams {
		if DataStreams[i].Name == "TWITCH_INGRESS" {
			return DataStreams[i]
		}
	}
	t.Fatal("TWITCH_INGRESS stream spec missing")
	return StreamSpec{}
}

type provisionedStream struct {
	replicas     int
	storage      jsapi.StorageType
	subjects     []string
	batchPublish bool
}

func TestEnsureStreamsProvisionsTheWholeCatalogAsReplicatedStreams(t *testing.T) {
	f := newBusFixture(t)

	for _, spec := range catalogSpecs() {
		t.Run(spec.Name, func(t *testing.T) {
			stream, err := f.modern.Stream(context.Background(), spec.Name)
			require.NoError(t, err)
			cfg := stream.CachedInfo().Config
			want := provisionedStream{replicas: 3, storage: jsapi.FileStorage, subjects: spec.Subjects, batchPublish: spec.BatchPublish}
			if spec.Storage != 0 {
				want.storage = jsapi.MemoryStorage
			}

			got := provisionedStream{
				replicas: cfg.Replicas, storage: cfg.Storage,
				subjects: cfg.Subjects, batchPublish: cfg.AllowAtomicPublish && cfg.AllowBatchPublish,
			}

			assert.Equal(t, want, got, "the broker must hold the catalog stream exactly as specified, replicated three ways")
		})
	}
}

func TestEnsureStreamsConvergesDriftWithoutLosingMessages(t *testing.T) {
	f := newBusFixture(t)
	token := nuid.Next()
	spec := StreamSpec{Name: "TEST_DRIFT_" + token, Subjects: []string{"drift." + token + ".>"}, Replicas: 3, MaxAge: time.Hour}
	_, err := f.js.AddStream(&nats.StreamConfig{Name: spec.Name, Subjects: spec.Subjects, Replicas: 1, MaxAge: 2 * time.Hour})
	require.NoError(t, err)
	for range 3 {
		f.appendMessage(t, "drift."+token+".event", "{}")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, EnsureStreams(ctx, f.url, []StreamSpec{spec}, zap.NewNop()))

	info, err := f.js.StreamInfo(spec.Name)
	require.NoError(t, err)
	assert.Equal(t, [3]any{3, time.Hour, uint64(3)}, [3]any{info.Config.Replicas, info.Config.MaxAge, info.State.Msgs},
		"drift converges in place: replicas and max age follow the spec and no message is lost")
}

func TestEnsureStreamsLeavesRetentionMigrationsToTheOperator(t *testing.T) {
	f := newBusFixture(t)
	token := nuid.Next()
	spec := StreamSpec{Name: "TEST_MIGRATE_" + token, Subjects: []string{"migrate." + token + ".>"}, Replicas: 3}
	_, err := f.js.AddStream(&nats.StreamConfig{
		Name: spec.Name, Subjects: spec.Subjects, Replicas: 3, Retention: nats.WorkQueuePolicy,
	})
	require.NoError(t, err)
	f.appendMessage(t, "migrate."+token+".event", "{}")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = EnsureStreams(ctx, f.url, []StreamSpec{spec}, zap.NewNop())

	require.Error(t, err, "a retention-policy change must be refused")
	info, infoErr := f.js.StreamInfo(spec.Name)
	require.NoError(t, infoErr)
	assert.Equal(t, [2]any{nats.WorkQueuePolicy, uint64(1)}, [2]any{info.Config.Retention, info.State.Msgs},
		"the stream must be left exactly as it was")
}

func TestIngressStreamIsolatesLanesPerSubject(t *testing.T) {
	cfg := streamConfig(ingressStreamSpec(t))

	requireContract(t,
		contractClause{cfg.MaxMsgsPerSubject > 0, "ingress lanes need a per-subject cap so one lane cannot evict the others"},
		contractClause{cfg.MaxBytes > 0, "ingress stream still needs its global byte backstop"},
		contractClause{cfg.Duplicates > 0 && cfg.Duplicates <= time.Minute,
			fmt.Sprintf("duplicate window = %v, want a short non-zero dedup window", cfg.Duplicates)},
	)
}

func TestPartitionFlagOffKeepsThePrePartitionShape(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	lanes := IngressLaneSpecs()
	require.Len(t, lanes, 1, "the single legacy stream")
	legacy := lanes[0]

	requireContract(t,
		contractClause{legacy.Name == TwitchIngressStream.Name,
			fmt.Sprintf("legacy stream = %q, want %q", legacy.Name, TwitchIngressStream.Name)},
		contractClause{len(legacy.Subjects) == 2 && legacy.Subjects[0] == "twitch.ingress.event.>",
			fmt.Sprintf("legacy subjects = %v, want the event wildcard restored", legacy.Subjects)},
		contractClause{legacy.MaxBytes == 1<<30, fmt.Sprintf("legacy MaxBytes = %d, want the whole gigabyte", legacy.MaxBytes)},
	)
}

func TestDiscordSubjectConstantsMatchTheCatalog(t *testing.T) {
	assert.True(t, slices.Equal(DiscordIngressStream.Subjects, []string{
		ddiscord.SubjectEventMessage, ddiscord.SubjectEventMember, ddiscord.SubjectEventVoice,
		ddiscord.SubjectEventInteraction, ddiscord.SubjectEventAudit, ddiscord.SubjectEventGuild,
	}), "DISCORD_INGRESS subjects drifted from internal/domain/discord: %q", DiscordIngressStream.Subjects)
	assert.True(t, slices.Equal(DiscordOutgressStream.Subjects, []string{ddiscord.LaneMod, ddiscord.LaneDefault}),
		"DISCORD_OUTGRESS subjects drifted from internal/domain/discord: %q", DiscordOutgressStream.Subjects)
}
