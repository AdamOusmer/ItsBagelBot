// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import "testing"

// These tests pin the contract that makes streamForTopic's process-lifetime
// memo safe: resolution stays a pure function of (partition mode, catalog),
// answers are byte-stable across hit and miss paths, unknown subjects are
// negative-cached with exactly the resolver's error, and an in-process flip of
// NATS_INGRESS_PARTITION invalidates instead of serving a stale generation —
// which is what keeps TestPartitionFlagOffKeepsThePrePartitionShape honest no
// matter which partition-on test ran before it.

func TestStreamForTopicResolvesEveryCatalogSpec(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	for subject, want := range map[string]string{
		"data.users.updated":            BagelDataStream.Name,
		"twitch.ingress.event.premium":  TwitchIngressStream.Name,
		"twitch.ingress.status.authz":   TwitchIngressStream.Name,
		"twitch.ingress.event.standard": TwitchIngressStream.Name,
		"twitch.outgress.premium":       OutgressStream.Name,
		"twitch.outgress.standard":      OutgressStream.Name,
		"twitch.outgress.system":        OutgressSystemStream.Name,
		"twitch.ingress.retry.x":        TwitchIngressRetryStream.Name,
	} {
		requireStreamForTopic(t, subject, want)
	}
}

func TestStreamForTopicServesTheStandardPartitionWhenEnabled(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	requireStreamForTopic(t, "twitch.ingress.event.standard", TwitchIngressStandardStream.Name)
}

func TestStreamForTopicCacheFollowsThePartitionFlip(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	requireStreamForTopic(t, "twitch.ingress.event.standard", TwitchIngressStream.Name)
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	requireStreamForTopic(t, "twitch.ingress.event.standard", TwitchIngressStandardStream.Name)
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	requireStreamForTopic(t, "twitch.ingress.event.standard", TwitchIngressStream.Name)
}

// requireExactRefusal asserts streamForTopic answers an unknown subject with
// the resolver's own refusal, byte for byte.
func requireExactRefusal(t *testing.T, subject, wantName string, wantErr error) {
	t.Helper()
	gotName, gotErr := streamForTopic(subject)
	switch {
	case gotName != wantName:
		t.Fatalf("streamForTopic(%q) = %q, want name %q (err %v)", subject, gotName, wantName, gotErr)
	case gotErr == nil:
		t.Fatalf("streamForTopic(%q) = %q, nil error; want the resolver's refusal %v", subject, gotName, wantErr)
	case gotErr.Error() != wantErr.Error():
		t.Fatalf("streamForTopic(%q) refused with %q; want the resolver's exact text %q", subject, gotErr, wantErr)
	}
}

func TestStreamForTopicNegativeCacheMatchesResolverError(t *testing.T) {
	const unknown = "nobody.claims.this"

	directName, directErr := resolveStreamForTopic(unknown)
	if directErr == nil {
		t.Fatalf("resolveStreamForTopic(%q) = %q, want an error", unknown, directName)
	}

	requireExactRefusal(t, unknown, directName, directErr)
	for i := 0; i < 3; i++ {
		requireExactRefusal(t, unknown, "", directErr)
	}
}

func TestStreamForTopicRepeatsAreStable(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "off")
	for _, subject := range []string{
		"twitch.ingress.event.premium",
		"twitch.ingress.retry.x",
		"twitch.outgress.premium",
	} {
		want, wantErr := streamForTopic(subject)
		if wantErr != nil {
			t.Fatalf("streamForTopic(%q): %v", subject, wantErr)
		}
		for i := 0; i < 5; i++ {
			got, gotErr := streamForTopic(subject)
			if gotErr != nil || got != want {
				t.Fatalf("streamForTopic(%q) repeat %d = %q, %v; want %q, <nil>",
					subject, i, got, gotErr, want)
			}
		}
	}
}
