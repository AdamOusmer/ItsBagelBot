package k8s

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
		"worker_bus":    {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY"},
		"outgress_bus":  {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM", "TWITCH_INGRESS"},
	}
	owners := map[string][]string{
		"users_bus":    {"BAGEL_DATA"},
		"worker_bus":   {"TWITCH_INGRESS", "TWITCH_INGRESS_RETRY"},
		"outgress_bus": {"TWITCH_OUTGRESS", "TWITCH_OUTGRESS_SYSTEM"},
	}
	// Flow-control reply subjects are ack authority, not ordinary acks: for an
	// AckFlowControl consumer the server never subscribes to $JS.ACK, so this is
	// the only channel that reopens the delivery window — and the sequence guard
	// that bounds a normal ack does not apply to it. Every account that binds
	// such a consumer needs exactly one stream-scoped grant, and no account may
	// hold a wildcard one.
	flowControl := map[string][]string{
		"worker_bus": {"TWITCH_INGRESS"},
	}
	serviceUsers := []string{
		"users_bus", "commands_bus", "modules_bus", "loyalty_bus",
		"transactions_bus", "projector_bus", "worker_bus", "outgress_bus",
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
				flowStreams:     flowControl[user],
			})
			if !slices.Equal(got, want) {
				t.Fatalf("JetStream grants differ (-want +got):\nwant %v\n got %v", want, got)
			}
		})
	}
}

// TestAdminBenchmarkStreamPermissionsAreExact keeps the production capacity
// runner confined to its disposable stream. The admin retains its existing KV
// ownership, but may not gain wildcard mutation access to the BUS account.
func TestAdminBenchmarkStreamPermissionsAreExact(t *testing.T) {
	config := sourceFile{name: "nats-auth.conf"}.read(t)
	block, ok := (authConfig{body: config}).busUserBlocks(t)["admin_bus"]
	if !ok {
		t.Fatal("missing admin_bus authorization block")
	}

	var got []string
	for _, subject := range block.jetStreamSubjects() {
		if streamMutationPattern.MatchString(subject) || strings.Contains(subject, "R3_SHADOW_BENCH") {
			got = append(got, subject)
		}
	}
	want := []string{
		"$JS.API.STREAM.CREATE.KV_admin_lanes",
		"$JS.API.STREAM.CREATE.R3_SHADOW_BENCH",
		"$JS.API.STREAM.DELETE.R3_SHADOW_BENCH",
		"$JS.API.STREAM.LEADER.STEPDOWN.R3_SHADOW_BENCH",
		"$JS.API.STREAM.UPDATE.KV_admin_lanes",
		// The harness's consumer role answers flow control on the bench stream;
		// scoped there and nowhere else, like the mutation verbs above.
		"$JS.FC.R3_SHADOW_BENCH.>",
		"$JS.EVENT.ADVISORY.STREAM.LEADER_ELECTED.R3_SHADOW_BENCH",
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("admin stream mutation grants differ (-want +got):\nwant %v\n got %v", want, got)
	}
}

// TestRuntimeStreamOwnershipMatchesACL keeps startup reconciliation aligned
// with the three identities that receive STREAM.CREATE/UPDATE above.
func TestRuntimeStreamOwnershipMatchesACL(t *testing.T) {
	mainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	check := streamOwnershipCheck{
		want: map[string]string{
			"users": "[]bus.StreamSpec{bus.BagelDataStream}",
			// Both ingress streams: worker_bus holds STREAM.CREATE/UPDATE for
			// TWITCH_INGRESS_RETRY, and a stream that is granted to an owner but
			// reconciled by nobody simply never exists — the delayed-redelivery
			// lane would fail open, dropping every retry with no error.
			"sesame":   "[]bus.StreamSpec{bus.TwitchIngressStream, bus.TwitchIngressRetryStream}",
			"outgress": "[]bus.StreamSpec{bus.OutgressStream, bus.OutgressSystemStream}",
		},
		seen: make(map[string]bool, 3),
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

type streamGrants struct {
	consumerStreams []string
	ownedStreams    []string
	flowStreams     []string
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
	for _, stream := range grants.flowStreams {
		set["$JS.FC."+stream+".>"] = struct{}{}
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
