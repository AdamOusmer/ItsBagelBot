// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const jetStreamAPI = "$JS.API."

var streamMutationPattern = regexp.MustCompile(`^\$JS\.API\.STREAM\.(CREATE|UPDATE|DELETE|LEADER\.STEPDOWN)\.`)

var flowControlStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
}

var pullFetchStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
}

var coordinationBuckets = map[string][]string{
	"outgress_bus": {"outgress_rate", "outgress_batch", "outgress_pause"},
	"deployer_bus": {"DEPLOY_RUNS"},
}

type streamGrants struct {
	consumerStreams []string
	ownedStreams    []string
	flowControl     []string
	pullFetch       []string
}

func expectedJetStreamSubjects(grants streamGrants) []string {
	set := make(map[string]struct{})
	for _, stream := range grants.flowControl {
		set["$JS.FC."+stream+".>"] = struct{}{}
	}
	for _, stream := range grants.pullFetch {
		set[jetStreamAPI+"CONSUMER.MSG.NEXT."+stream+".>"] = struct{}{}
	}
	for _, stream := range grants.consumerStreams {
		for _, subject := range []string{
			jetStreamAPI + "STREAM.INFO." + stream,
			jetStreamAPI + "CONSUMER.INFO." + stream + ".>",
			jetStreamAPI + "CONSUMER.CREATE." + stream + ".>",
			jetStreamAPI + "CONSUMER.DURABLE.CREATE." + stream + ".>",
			jetStreamAPI + "CONSUMER.DELETE." + stream + ".>",
			"$JS.ACK." + stream + ".>",
			"$JS.ACK.*.*." + stream + ".>",
		} {
			set[subject] = struct{}{}
		}
	}
	for _, stream := range grants.ownedStreams {
		set[jetStreamAPI+"STREAM.INFO."+stream] = struct{}{}
		set[jetStreamAPI+"STREAM.CREATE."+stream] = struct{}{}
		set[jetStreamAPI+"STREAM.UPDATE."+stream] = struct{}{}
	}
	return sortedKeys(set)
}

func coordinationSubjects(user string) []string {
	buckets := coordinationBuckets[user]
	if len(buckets) == 0 {
		return nil
	}
	subjects := []string{jetStreamAPI + "INFO"}
	for _, bucket := range buckets {
		for _, verb := range []string{"STREAM.INFO.", "STREAM.CREATE.", "STREAM.UPDATE.", "STREAM.MSG.GET.", "DIRECT.GET."} {
			subjects = append(subjects, jetStreamAPI+verb+"KV_"+bucket)
		}
		subjects = append(subjects, jetStreamAPI+"DIRECT.GET.KV_"+bucket+".>")
	}
	return subjects
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func TestServiceBusJetStreamPermissionsAreExact(t *testing.T) {
	blocks := busUserBlocks(t)

	consumers := map[string][]string{
		"users_bus":            {"BAGEL_DATA"},
		"commands_bus":         {"BAGEL_DATA"},
		"modules_bus":          {"BAGEL_DATA"},
		"loyalty_bus":          {"BAGEL_DATA"},
		"projector_bus":        {"BAGEL_DATA", "TWITCH_INGRESS"},
		"worker_bus":           {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
		"outgress_bus":         {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM", "TWITCH_INGRESS"},
		"discord_engine_bus":   {"DISCORD_INGRESS", "BAGEL_DATA", "TWITCH_INGRESS"},
		"discord_outgress_bus": {"DISCORD_OUTGRESS"},
		"discord_ingress_bus":  {},
		"deployer_bus":         {},
	}
	owners := map[string][]string{
		"users_bus":            {"BAGEL_DATA", "BAGEL_DLQ"},
		"worker_bus":           {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
		"projector_bus":        {"BAGEL_DATA", "BAGEL_DLQ", "TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
		"outgress_bus":         {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM"},
		"discord_engine_bus":   {"DISCORD_INGRESS"},
		"discord_outgress_bus": {"DISCORD_OUTGRESS"},
	}
	serviceUsers := []string{
		"users_bus", "commands_bus", "modules_bus", "loyalty_bus",
		"projector_bus", "worker_bus", "outgress_bus",
		"twitch_ingress_bus", "dashboard_bus",
		"discord_ingress_bus", "discord_engine_bus", "discord_outgress_bus",
		"deployer_bus",
	}

	for _, user := range serviceUsers {
		t.Run(user, func(t *testing.T) {
			block, ok := blocks[user]
			require.True(t, ok, "missing %s authorization block", user)
			want := append(expectedJetStreamSubjects(streamGrants{
				consumerStreams: consumers[user],
				ownedStreams:    owners[user],
				flowControl:     flowControlStreams[user],
				pullFetch:       pullFetchStreams[user],
			}), coordinationSubjects(user)...)
			slices.Sort(want)

			assert.Equal(t, want, block.jetStreamSubjects(), "JetStream grants differ")
		})
	}
}

func TestAdminStreamMutationGrantsAreOnlyItsOwnKV(t *testing.T) {
	block, ok := busUserBlocks(t)["admin_bus"]
	require.True(t, ok, "missing admin_bus authorization block")

	var got []string
	for _, subject := range block.jetStreamSubjects() {
		if streamMutationPattern.MatchString(subject) {
			got = append(got, subject)
		}
		assert.False(t, strings.Contains(subject, "BENCH") || strings.Contains(subject, "SHADOW"),
			"admin_bus still grants a benchmark-stream subject: %s", subject)
	}

	assert.Equal(t, []string{
		"$JS.API.STREAM.CREATE.KV_admin_lanes",
		"$JS.API.STREAM.LEADER.STEPDOWN.>",
		"$JS.API.STREAM.UPDATE.KV_admin_lanes",
	}, got)
}

func TestCoordinationBucketPublishIsolation(t *testing.T) {
	blocks := busUserBlocks(t)
	for owner, buckets := range coordinationBuckets {
		for _, bucket := range buckets {
			subject := "$KV." + bucket + ".>"
			for user, block := range blocks {
				assert.Equal(t, user == owner, block.grants(subject), "%s permission for %s", user, subject)
			}
		}
	}
}

func TestRuntimeStreamOwnershipMatchesACL(t *testing.T) {
	want := map[string][]string{
		"users":     {"[]bus.StreamSpec{bus.BagelDataStream, bus.BagelDeadLetterStream}"},
		"sesame":    {"bus.IngressLaneSpecs()"},
		"projector": {"append([]bus.StreamSpec{bus.BagelDataStream, bus.BagelDeadLetterStream}, bus.IngressLaneSpecs()...)"},
		"outgress": {
			"[]bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}",
		},
		"discord-engine":   {"[]bus.StreamSpec{bus.DiscordIngressStream}"},
		"discord-outgress": {"[]bus.StreamSpec{bus.DiscordOutgressStream}"},
	}
	mainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "main.go"))
	require.NoError(t, err)
	groupedMainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "*", "main.go"))
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, name := range slices.Concat(mainFiles, groupedMainFiles) {
		body := readText(t, name)
		if !strings.Contains(body, "bus.EnsureStreams(") {
			continue
		}
		dir := filepath.Dir(name)
		service := filepath.Base(dir)
		if filepath.Base(filepath.Dir(dir)) == "discord" {
			service = "discord-" + service
		}
		snippets, owned := want[service]
		if !assert.True(t, owned, "%s reconciles streams but has no stream-owner ACL", service) {
			continue
		}
		for _, snippet := range snippets {
			assert.Contains(t, body, snippet, "%s does not reconcile only its owned stream(s)", service)
		}
		seen[service] = true
	}

	for service := range want {
		assert.True(t, seen[service], "stream owner %s does not call EnsureStreams", service)
	}
}
