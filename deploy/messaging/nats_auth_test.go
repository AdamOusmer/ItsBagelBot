// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const jetStreamAPI = "$JS.API."

var streamMutationPattern = regexp.MustCompile(`^\$JS\.API\.STREAM\.(CREATE|UPDATE|DELETE|LEADER\.STEPDOWN)\.`)

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
			if !ok {
				t.Fatalf("missing %s authorization block", user)
			}
			got := block.jetStreamSubjects()
			want := expectedJetStreamSubjects(streamGrants{
				consumerStreams: consumers[user],
				ownedStreams:    owners[user],
				flowControl:     flowControlStreams[user],
				pullFetch:       pullFetchStreams[user],
			})
			want = append(want, coordinationSubjects(user)...)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("JetStream grants differ (-want +got):\nwant %v\n got %v", want, got)
			}
		})
	}
}

func TestAdminStreamMutationGrantsAreOnlyItsOwnKV(t *testing.T) {
	block, ok := busUserBlocks(t)["admin_bus"]
	if !ok {
		t.Fatal("missing admin_bus authorization block")
	}

	var got []string
	for _, subject := range block.jetStreamSubjects() {
		if streamMutationPattern.MatchString(subject) {
			got = append(got, subject)
		}
		if strings.Contains(subject, "BENCH") || strings.Contains(subject, "SHADOW") {
			t.Errorf("admin_bus still grants a benchmark-stream subject: %s", subject)
		}
	}
	want := []string{
		"$JS.API.STREAM.CREATE.KV_admin_lanes",
		"$JS.API.STREAM.LEADER.STEPDOWN.>",
		"$JS.API.STREAM.UPDATE.KV_admin_lanes",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("admin stream mutation grants differ (-want +got):\nwant %v\n got %v", want, got)
	}
}

func TestRuntimeStreamOwnershipMatchesACL(t *testing.T) {
	mainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	groupedMainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	check := streamOwnershipCheck{
		want: map[string][]string{
			"users":     {"[]bus.StreamSpec{bus.BagelDataStream, bus.BagelDeadLetterStream}"},
			"sesame":    {"bus.IngressLaneSpecs()"},
			"projector": {"append([]bus.StreamSpec{bus.BagelDataStream, bus.BagelDeadLetterStream}, bus.IngressLaneSpecs()...)"},
			"outgress": {
				"[]bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}",
			},
			"discord-engine":   {"[]bus.StreamSpec{bus.DiscordIngressStream}"},
			"discord-outgress": {"[]bus.StreamSpec{bus.DiscordOutgressStream}"},
		},
		seen: make(map[string]bool, 6),
	}

	for _, name := range mainFiles {
		check.inspect(t, sourceFile{name: name})
	}
	for _, name := range groupedMainFiles {
		check.inspect(t, sourceFile{name: name})
	}
	for service := range check.want {
		if !check.seen[service] {
			t.Errorf("stream owner %s does not call EnsureStreams", service)
		}
	}
}

type streamOwnershipCheck struct {
	want map[string][]string
	seen map[string]bool
}

type sourceFile struct {
	name string
}

var flowControlStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
}

var pullFetchStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
}

type streamGrants struct {
	consumerStreams []string
	ownedStreams    []string
	flowControl     []string
	pullFetch       []string
}

func (c *streamOwnershipCheck) inspect(t *testing.T, file sourceFile) {
	t.Helper()
	body := file.read(t)
	if !strings.Contains(body, "bus.EnsureStreams(") {
		return
	}
	dir := filepath.Dir(file.name)
	service := filepath.Base(dir)
	if filepath.Base(filepath.Dir(dir)) == "discord" {
		service = "discord-" + service
	}
	snippets, ok := c.want[service]
	if !ok {
		t.Errorf("%s reconciles streams but has no stream-owner ACL", service)
		return
	}
	for _, snippet := range snippets {
		if !strings.Contains(body, snippet) {
			t.Errorf("%s does not reconcile only its owned stream(s): want %s", service, snippet)
		}
	}
	c.seen[service] = true
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

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func (f sourceFile) read(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
