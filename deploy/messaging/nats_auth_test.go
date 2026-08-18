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

var busUserPattern = regexp.MustCompile(`(?m)^[ \t]*user: "([a-z_]+_bus)"`)
var jsSubjectPattern = regexp.MustCompile(`"(\$JS[^"]+)"`)
var streamMutationPattern = regexp.MustCompile(`^\$JS\.API\.STREAM\.(CREATE|UPDATE|DELETE|LEADER\.STEPDOWN)\.`)

// TestServiceBusJetStreamPermissionsAreExact is the regression gate for the
// BUS-account blast radius. A broad $JS.> grant, an extra stream, or a newly
// added management verb must be reviewed here instead of silently reaching
// every stream in the account.
func TestServiceBusJetStreamPermissionsAreExact(t *testing.T) {
	config := sourceFile{name: "nats-auth.conf"}.read(t)
	blocks := (authConfig{body: config}).busUserBlocks(t)

	consumers := map[string][]string{
		"users_bus":     {"BAGEL_DATA"},
		"commands_bus":  {"BAGEL_DATA"},
		"modules_bus":   {"BAGEL_DATA"},
		"loyalty_bus":   {"BAGEL_DATA"},
		"projector_bus": {"BAGEL_DATA", "TWITCH_INGRESS"},
		"worker_bus":    {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
		"outgress_bus":  {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM", "TWITCH_INGRESS"},
	}
	owners := map[string][]string{
		"users_bus":  {"BAGEL_DATA"},
		"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY", "TWITCH_INGRESS_STANDARD"},
		// The projector self-provisions its inputs rather than depending on
		// users/sesame boot order; identical catalog specs make the concurrent
		// reconciles converge.
		"projector_bus": {"BAGEL_DATA", "TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
		"outgress_bus":  {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM"},
	}
	serviceUsers := []string{
		"users_bus", "commands_bus", "modules_bus", "loyalty_bus",
		"projector_bus", "worker_bus", "outgress_bus",
		"twitch_ingress_bus", "dashboard_bus",
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
			if !slices.Equal(got, want) {
				t.Fatalf("JetStream grants differ (-want +got):\nwant %v\n got %v", want, got)
			}
		})
	}
}

// TestAdminStreamMutationGrantsAreOnlyItsOwnKV holds admin_bus to the one
// stream it actually owns, plus the leader-stepdown verb that implements the
// manual leader-spread policy after hub rolls. It used to also carry
// create/update/delete and $JS.FC on a fixed benchmark stream name so a load
// rig could run without a config push. That is standing ack-floor and
// stream-mutation authority for a workload that is not running, and it
// outlived the rig by itself — which is exactly how a permission becomes
// permanent. A future load test brings its own grant for the duration of its
// run. Stepdown is the deliberate exception: it moves leaders and nothing
// else, and the spread must be restorable during an incident without a config
// push.
func TestAdminStreamMutationGrantsAreOnlyItsOwnKV(t *testing.T) {
	config := sourceFile{name: "nats-auth.conf"}.read(t)
	block, ok := (authConfig{body: config}).busUserBlocks(t)["admin_bus"]
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

// TestRuntimeStreamOwnershipMatchesACL keeps startup reconciliation aligned
// with the identities that receive STREAM.CREATE/UPDATE above.
func TestRuntimeStreamOwnershipMatchesACL(t *testing.T) {
	mainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Each owner's managed-stream shape now comes from its recipes binding's
	// Manages()-style method (see recipes/MAP.md's "manage" rows) instead of a
	// literal []bus.StreamSpec — the binding is generated from the fleet
	// manifest, so it is the single source of truth for who owns what.
	check := streamOwnershipCheck{
		want: map[string]string{
			"users":     "k.Z5G2Z()",
			"sesame":    "k.ZFLOB()",
			"projector": "k.ZNT6V()",
			"outgress":  "k.ZWMQF()",
		},
		seen: make(map[string]bool, 4),
	}

	for _, name := range mainFiles {
		check.inspect(t, sourceFile{name: name})
	}
	for service := range check.want {
		if !check.seen[service] {
			t.Errorf("stream owner %s does not call EnsureStreams", service)
		}
	}
}

type streamOwnershipCheck struct {
	want map[string]string
	seen map[string]bool
}

type sourceFile struct {
	name string
}

// flowControlStreams names, per user, the streams whose consumers acknowledge
// through the AckFlowControl reply subject rather than $JS.ACK. Publishing to
// $JS.FC is how such a consumer acks at all, so the grant is mandatory — and it
// is ack-equivalent authority on that consumer, which is why it is scoped to a
// single stream and asserted here rather than folded into the per-stream set.
var flowControlStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
}

// pullFetchStreams names, per user, the streams whose lanes may bind a
// shared-durable pull consumer. MSG.NEXT is that consumer's fetch verb and no
// push consumer ever sends it, so it is listed separately rather than folded
// into the per-stream consumer set: granting it to a service that only binds
// push consumers is authority for a call that service never makes, and NOT
// granting it to one that pulls is a silent zero-delivery lane rather than a
// visible error. Only the hot ingress lanes qualify for receipt-level
// consumption (pkg/bus isHotIngressLane), and sesame is their only consumer.
var pullFetchStreams = map[string][]string{
	"worker_bus": {"TWITCH_INGRESS", "TWITCH_INGRESS_STANDARD"},
}

type streamGrants struct {
	consumerStreams []string
	ownedStreams    []string
	flowControl     []string
	pullFetch       []string
}

type authConfig struct {
	body string
}

type busUserBlock struct {
	body string
}

func (c *streamOwnershipCheck) inspect(t *testing.T, file sourceFile) {
	t.Helper()
	body := file.read(t)
	if !strings.Contains(body, "bus.EnsureStreams(") {
		return
	}
	service := filepath.Base(filepath.Dir(file.name))
	snippet, ok := c.want[service]
	if !ok {
		t.Errorf("%s reconciles streams but has no stream-owner ACL", service)
		return
	}
	if !strings.Contains(body, snippet) {
		t.Errorf("%s does not reconcile only its owned stream(s): want %s", service, snippet)
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

func (b busUserBlock) jetStreamSubjects() []string {
	set := make(map[string]struct{})
	for _, match := range jsSubjectPattern.FindAllStringSubmatch(b.body, -1) {
		set[match[1]] = struct{}{}
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

func (c authConfig) busUserBlocks(t *testing.T) map[string]busUserBlock {
	t.Helper()
	matches := busUserPattern.FindAllStringSubmatchIndex(c.body, -1)
	blocks := make(map[string]busUserBlock, len(matches))
	for i, match := range matches {
		end := len(c.body)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		blocks[c.body[match[2]:match[3]]] = busUserBlock{body: c.body[match[0]:end]}
	}
	return blocks
}

func (f sourceFile) read(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
