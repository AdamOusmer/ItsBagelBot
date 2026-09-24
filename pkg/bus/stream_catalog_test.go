// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	ddiscord "ItsBagelBot/internal/domain/discord"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
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

func catalogIndex(t *testing.T, name string) int {
	t.Helper()
	for i := range DataStreams {
		if DataStreams[i].Name == name {
			return i
		}
	}
	t.Fatalf("stream %s missing from the catalog", name)
	return -1
}

func TestOutgressStreamIsPerishableWorkQueue(t *testing.T) {
	cfg := streamConfig(OutgressStream)

	if cfg.Retention != jsapi.WorkQueuePolicy {
		t.Fatalf("retention = %v, want work queue", cfg.Retention)
	}
	if cfg.MaxAge != 5*time.Second {
		t.Fatalf("max age = %v, want 5s", cfg.MaxAge)
	}
	if cfg.Duplicates > cfg.MaxAge {
		t.Fatalf("duplicate window %v exceeds max age %v", cfg.Duplicates, cfg.MaxAge)
	}
}

func TestOutgressSystemStreamIsDurableWorkQueue(t *testing.T) {
	cfg := streamConfig(OutgressSystemStream)

	if cfg.Retention != jsapi.WorkQueuePolicy {
		t.Fatalf("retention = %v, want work queue (ack removes, not replay)", cfg.Retention)
	}
	if cfg.MaxAge <= 5*time.Second {
		t.Fatalf("max age = %v, want longer than the chat lane's 5s", cfg.MaxAge)
	}
	if cfg.Duplicates > cfg.MaxAge {
		t.Fatalf("duplicate window %v exceeds max age %v", cfg.Duplicates, cfg.MaxAge)
	}
}

func TestCatalogStreamsClaimDisjointSubjects(t *testing.T) {
	specs := fleetStreamSpecs()
	for i := range specs {
		for j := i + 1; j < len(specs); j++ {
			requireDisjointSubjects(t, specs[i], specs[j])
		}
	}
}

func requireDisjointSubjects(t *testing.T, first, second StreamSpec) {
	t.Helper()
	for _, a := range first.Subjects {
		for _, b := range second.Subjects {
			if matchSubject(a, b) || matchSubject(b, a) {
				t.Fatalf("streams %s (%q) and %s (%q) both claim the same subject space",
					first.Name, a, second.Name, b)
			}
		}
	}
}

func TestIngressLanesResolveToTheirPartitions(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	for subject, want := range map[string]string{
		"twitch.ingress.event.standard":       TwitchIngressStandardStream.Name,
		"twitch.ingress.event.premium":        TwitchIngressStream.Name,
		"twitch.ingress.event.stream":         TwitchIngressStream.Name,
		"twitch.ingress.status.authz.revoked": TwitchIngressStream.Name,
		"twitch.ingress.retry.standard":       TwitchIngressRetryStream.Name,
	} {
		requireStreamForTopic(t, subject, want)
	}

	if got, err := streamForTopic("twitch.ingress.event.unknown"); err == nil {
		t.Fatalf("streamForTopic(unknown lane) = %q, want a refusal; a wildcard is back in the catalog", got)
	}
}

func requireStreamForTopic(t *testing.T, subject, want string) {
	t.Helper()
	got, err := streamForTopic(subject)
	if err != nil {
		t.Fatalf("streamForTopic(%q): %v", subject, err)
	}
	if got != want {
		t.Fatalf("stream for %q = %q, want %q", subject, got, want)
	}
}

func TestPartitionFlagOffKeepsThePrePartitionShape(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	lanes := IngressLaneSpecs()
	if len(lanes) != 1 {
		t.Fatalf("lane specs with the partition off = %#v, want the single legacy stream", lanes)
	}
	legacy := lanes[0]
	requireContract(t,
		contractClause{legacy.Name == TwitchIngressStream.Name,
			fmt.Sprintf("legacy stream = %q, want %q", legacy.Name, TwitchIngressStream.Name)},
		contractClause{len(legacy.Subjects) == 2 && legacy.Subjects[0] == "twitch.ingress.event.>",
			fmt.Sprintf("legacy subjects = %v, want the event wildcard restored", legacy.Subjects)},
		contractClause{legacy.MaxBytes == 1<<30,
			fmt.Sprintf("legacy MaxBytes = %d, want the whole gigabyte", legacy.MaxBytes)},
	)
	requireStreamForTopic(t, "twitch.ingress.event.standard", TwitchIngressStream.Name)
	requireStreamForTopic(t, "twitch.ingress.event.premium", TwitchIngressStream.Name)
}

func TestSystemSubjectResolvesToSystemStream(t *testing.T) {
	got, err := streamForTopic("twitch.outgress.system")
	if err != nil {
		t.Fatalf("streamForTopic: %v", err)
	}
	if got != OutgressSystemStream.Name {
		t.Fatalf("stream = %q, want %q", got, OutgressSystemStream.Name)
	}
	for _, chat := range []string{"twitch.outgress.premium", "twitch.outgress.standard"} {
		requireStreamForTopic(t, chat, OutgressStream.Name)
	}
}

func TestOutgressStreamHasSingleReconciler(t *testing.T) {
	for _, spec := range DataStreams {
		if spec.Name == OutgressStream.Name {
			t.Fatal("outgress stream must be reconciled only by outgress")
		}
	}
}

func TestYouTubeStreamsResolveToTheirSpecs(t *testing.T) {
	for subject, want := range map[string]string{
		"youtube.outgress.premium":         YouTubeOutgressStream.Name,
		"youtube.outgress.standard":        YouTubeOutgressStream.Name,
		"youtube.ingress.event.stream":     YouTubeIngressStream.Name,
		"youtube.ingress.event.premium":    YouTubeIngressStream.Name,
		"youtube.ingress.event.standard":   YouTubeIngressStream.Name,
		"youtube.ingress.status.chat.up":   YouTubeIngressStream.Name,
		"youtube.ingress.status.chat.down": YouTubeIngressStream.Name,
	} {
		got, err := streamForTopic(subject)
		if err != nil {
			t.Fatalf("streamForTopic(%q): %v", subject, err)
		}
		if got != want {
			t.Fatalf("stream for %q = %q, want %q", subject, got, want)
		}
	}
}

func TestIngressStreamIsolatesLanesPerSubject(t *testing.T) {
	cfg := streamConfig(ingressStreamSpec(t))
	if cfg.MaxMsgsPerSubject <= 0 {
		t.Fatal("ingress lanes need a per-subject cap so one lane cannot evict the others")
	}
	if cfg.MaxBytes <= 0 {
		t.Fatal("ingress stream still needs its global byte backstop")
	}
	if cfg.Duplicates <= 0 || cfg.Duplicates > time.Minute {
		t.Fatalf("duplicate window = %v, want a short non-zero dedup window", cfg.Duplicates)
	}
}

func TestEveryFleetStreamEnablesBatchPublishing(t *testing.T) {
	for _, spec := range fleetStreamSpecs() {
		if !spec.BatchPublish {
			t.Fatalf("stream %s does not enable shared batch publishing", spec.Name)
		}
	}
}

func TestEveryFleetStreamIsReplicated(t *testing.T) {
	for _, spec := range fleetStreamSpecs() {
		if got := streamConfig(spec).Replicas; got != 3 {
			t.Fatalf("stream %s replicas = %d, want 3", spec.Name, got)
		}
	}

	if got := streamConfig(StreamSpec{Name: "X", Subjects: []string{"x.>"}}).Replicas; got != 1 {
		t.Fatalf("default replicas = %d, want 1", got)
	}

	want := streamConfig(ingressStreamSpec(t))
	drifted := want
	drifted.Replicas = 1
	if streamMatches(drifted, want) {
		t.Fatal("streamMatches ignored a replica drift; live R1 would never converge to R3")
	}
}

func TestFleetStreamsCarryNoPlacement(t *testing.T) {
	for _, spec := range fleetStreamSpecs() {
		if cfg := streamConfig(spec); cfg.Placement != nil {
			t.Fatalf("R3 stream %s carries placement %v; an ordinal tag cannot satisfy three peers",
				spec.Name, cfg.Placement.Tags)
		}
	}

	want := streamConfig(ingressStreamSpec(t))
	stale := want
	stale.Placement = &jsapi.Placement{Tags: []string{"nats-0"}}
	if streamMatches(stale, want) {
		t.Fatal("streamMatches ignored a stale placement on a live stream")
	}
	if streamMatches(want, stale) {
		t.Fatal("streamMatches ignored a placement the spec asks for but the stream lacks")
	}
}

func TestStreamConfigEmitsServerSentinels(t *testing.T) {
	cfg := streamConfig(BagelDataStream)
	if cfg.MaxMsgsPerSubject != -1 {
		t.Fatalf("max msgs per subject = %d, want the server's -1 sentinel", cfg.MaxMsgsPerSubject)
	}
	for name, got := range map[string]int64{
		"max_msgs":      cfg.MaxMsgs,
		"max_msg_size":  int64(cfg.MaxMsgSize),
		"max_consumers": int64(cfg.MaxConsumers),
	} {
		if got != -1 {
			t.Fatalf("%s = %d, want the server's -1 sentinel", name, got)
		}
	}

	unnormalized := cfg
	unnormalized.MaxMsgsPerSubject = 0
	if streamMatches(cfg, unnormalized) {
		t.Fatal("streamMatches treats 0 and -1 as equal; a per-subject cap could then be silently dropped")
	}
}

func TestRetryStreamCarriesTheSchedulePrerequisites(t *testing.T) {
	cfg := streamConfig(TwitchIngressRetryStream)

	requireContract(t,
		contractClause{cfg.AllowMsgSchedules,
			"retry lane does not enable message schedules; there is no delay primitive left"},
		contractClause{cfg.AllowMsgTTL,
			"Nats-Schedule-TTL is rejected when message TTLs are disabled"},
		contractClause{cfg.AllowRollup && !cfg.DenyPurge,
			"scheduling forces rollups and permits purge; emitting the requested shape avoids permanent drift"},
		contractClause{cfg.Discard == jsapi.DiscardOld,
			"message scheduling cannot use discard new"},
	)
	if cfg.MaxAge < time.Minute {
		t.Fatalf("max age = %v, too tight to hold a delayed retry", cfg.MaxAge)
	}
	if got, err := streamForTopic("twitch.ingress.retry.premium"); err != nil || got != cfg.Name {
		t.Fatalf("streamForTopic(retry) = %q, %v; want %q", got, err, cfg.Name)
	}
}

func TestLegacySpecEnumsMatchModernEnums(t *testing.T) {
	for _, retention := range []struct {
		legacy nats.RetentionPolicy
		modern jsapi.RetentionPolicy
	}{
		{nats.WorkQueuePolicy, jsapi.WorkQueuePolicy},
		{nats.InterestPolicy, jsapi.InterestPolicy},
		{nats.LimitsPolicy, jsapi.LimitsPolicy},
	} {
		if jsapi.RetentionPolicy(retention.legacy) != retention.modern {
			t.Fatal("retention enums diverged between the legacy and modern clients")
		}
	}
	if streamConfig(StreamSpec{Name: "M", Subjects: []string{"m.>"}, Storage: nats.MemoryStorage}).Storage != jsapi.MemoryStorage {
		t.Fatal("memory storage did not survive the spec conversion")
	}
	if streamConfig(StreamSpec{Name: "F", Subjects: []string{"f.>"}}).Storage != jsapi.FileStorage {
		t.Fatal("the default storage tier is no longer file")
	}
}

func TestFleetStreamStorageTiersAreExplicit(t *testing.T) {
	memory := map[string]bool{
		TwitchIngressStream.Name:         true,
		TwitchIngressStandardStream.Name: true,
		TwitchIngressRetryStream.Name:    true,
		OutgressStream.Name:              true,
		YouTubeOutgressStream.Name:       true,
		YouTubeIngressStream.Name:        true,
	}
	for _, spec := range fleetStreamSpecs() {
		want := jsapi.FileStorage
		if memory[spec.Name] {
			want = jsapi.MemoryStorage
		}
		if got := streamConfig(spec).Storage; got != want {
			t.Fatalf("stream %s storage = %v, want %v", spec.Name, got, want)
		}
	}
}

func TestDiscordSubjectConstantsMatchTheCatalog(t *testing.T) {
	wantIngress := []string{
		ddiscord.SubjectEventMessage,
		ddiscord.SubjectEventMember,
		ddiscord.SubjectEventVoice,
		ddiscord.SubjectEventInteraction,
		ddiscord.SubjectEventAudit,
		ddiscord.SubjectEventGuild,
	}
	if !slices.Equal(DiscordIngressStream.Subjects, wantIngress) {
		t.Fatalf("DISCORD_INGRESS subjects drifted from internal/domain/discord:\n stream = %q\n consts = %q",
			DiscordIngressStream.Subjects, wantIngress)
	}
	wantOutgress := []string{ddiscord.LaneMod, ddiscord.LaneDefault}
	if !slices.Equal(DiscordOutgressStream.Subjects, wantOutgress) {
		t.Fatalf("DISCORD_OUTGRESS subjects drifted from internal/domain/discord:\n stream = %q\n consts = %q",
			DiscordOutgressStream.Subjects, wantOutgress)
	}
}
