package k8s

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type workloadManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				TopologySpreadConstraints []struct {
					MinDomains        int      `yaml:"minDomains"`
					TopologyKey       string   `yaml:"topologyKey"`
					WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
					MatchLabelKeys    []string `yaml:"matchLabelKeys"`
				} `yaml:"topologySpreadConstraints"`
				Affinity struct {
					NodeAffinity struct {
						Required struct {
							NodeSelectorTerms []struct {
								MatchExpressions []struct {
									Key      string   `yaml:"key"`
									Operator string   `yaml:"operator"`
									Values   []string `yaml:"values"`
								} `yaml:"matchExpressions"`
							} `yaml:"nodeSelectorTerms"`
						} `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
					} `yaml:"nodeAffinity"`
				} `yaml:"affinity"`
			} `yaml:"spec"`
		} `yaml:"template"`
		VolumeClaimTemplates []struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		} `yaml:"volumeClaimTemplates"`
	} `yaml:"spec"`
}

func loadDeployment(t *testing.T, filename, name string) workloadManifest {
	t.Helper()

	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	for {
		var manifest workloadManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if manifest.Kind == "Deployment" && manifest.Metadata.Name == name {
			return manifest
		}
	}

	t.Fatalf("%s Deployment is missing from %s", name, filename)
	return workloadManifest{}
}

func hostnameSpread(t *testing.T, manifest workloadManifest) struct {
	MinDomains        int      `yaml:"minDomains"`
	TopologyKey       string   `yaml:"topologyKey"`
	WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
	MatchLabelKeys    []string `yaml:"matchLabelKeys"`
} {
	t.Helper()
	for _, constraint := range manifest.Spec.Template.Spec.TopologySpreadConstraints {
		if constraint.TopologyKey == "kubernetes.io/hostname" {
			return constraint
		}
	}
	t.Fatalf("%s has no hostname topology spread constraint", manifest.Metadata.Name)
	return struct {
		MinDomains        int      `yaml:"minDomains"`
		TopologyKey       string   `yaml:"topologyKey"`
		WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
		MatchLabelKeys    []string `yaml:"matchLabelKeys"`
	}{}
}

// TestSpreadIsPreferredNeverBlocking asserts the placement policy the fleet
// settled on after 2026-07-27: spread hard enough to survive a rollout, never
// hard enough to withhold a replica.
//
// These were DoNotSchedule with minDomains, which is a correct-looking
// constraint that fails badly the moment a node is gone for longer than a few
// minutes. When node5 died, thirteen deployments sat at 2/3 with a permanently
// Pending third replica: the scheduler could not place it without exceeding
// maxSkew against a domain that no longer had a schedulable node. Capacity was
// withheld for hours to preserve a spread that could not be achieved anyway.
//
// minDomains was worse than that. It is only valid alongside DoNotSchedule, and
// if it ever exceeds the number of eligible domains, skew is measured against a
// global minimum of zero and EVERY replica becomes unschedulable. It sat at 4
// while the fleet had four app nodes and only kept working after the move
// because cordoned nodes still count as domains (nodeTaintsPolicy defaults to
// Ignore); deleting them would have taken those services to zero replicas.
//
// ScheduleAnyway keeps the scheduler preferring an even spread while letting a
// replica land anywhere rather than nowhere, and matchLabelKeys still scopes the
// skew to the incoming ReplicaSet so each rollout re-spreads. Kubernetes does
// not rebalance running pods, so a returning node is repopulated by the next
// rollout rather than immediately, which is the accepted trade.
//
// Quorum services are the deliberate exception and are NOT listed here: nats and
// valkey keep required podAntiAffinity, because two members of a three-member
// quorum sharing a node means one node loss costs the quorum.
func TestSpreadIsPreferredNeverBlocking(t *testing.T) {
	for _, tt := range []struct{ file, name string }{
		{"console-admin.yaml", "console-admin"},
		{"notifications.yaml", "notifications"},
		{"transactions.yaml", "transactions"},
		{"twitch-ingress.yaml", "twitch-ingress"},
		{"sesame.yaml", "sesame"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			constraint := hostnameSpread(t, loadDeployment(t, tt.file, tt.name))
			if constraint.WhenUnsatisfiable != "ScheduleAnyway" {
				t.Errorf("hostname spread is %q; must be ScheduleAnyway so a missing node cannot withhold a replica",
					constraint.WhenUnsatisfiable)
			}
			if !slices.Contains(constraint.MatchLabelKeys, "pod-template-hash") {
				t.Error("hostname spread must be scoped to the incoming ReplicaSet via matchLabelKeys")
			}
			if constraint.MinDomains != 0 {
				t.Errorf("minDomains = %d; it is only valid with DoNotSchedule and turns into a total outage when it exceeds the eligible domain count",
					constraint.MinDomains)
			}
		})
	}
}

// excludesWorkerPool captures the placement rule under test: a nodeAffinity
// match expression that keeps the workload off worker-role nodes.
//
// The key moved from itsbagelbot.dev/pool=worker-pool to
// itsbagelbot.dev/role=worker when node roles were unified onto one key
// (cp / node / worker). No worker-role node exists yet, so this guard is
// currently inert — it is kept expressed on the live key so that it starts
// holding the moment one is added, rather than silently pointing at a retired
// label that nothing would ever match.
func excludesWorkerPool(key, operator string, values []string) bool {
	return key == "itsbagelbot.dev/role" &&
		operator == "NotIn" &&
		slices.Contains(values, "worker")
}

// TestNoWorkloadSelectsNodesByHostname guards the failure mode this directory
// kept re-learning: a nodeAffinity that names individual hosts goes stale the
// moment the fleet changes shape, and a stale term is silently inert rather than
// loud. Retiring a node left rules reading "NotIn [worker1]" and "NotIn [node1]"
// long after both hosts were gone, each carrying a comment admitting it was
// already inert. Selecting on itsbagelbot.dev/role instead keeps the intent
// ("not the control plane", "not a burst worker") true across fleet changes.
//
// Only nodeAffinity is in scope. topologySpreadConstraints and podAntiAffinity
// legitimately use kubernetes.io/hostname as a topology KEY, which spreads
// across whatever hosts exist rather than naming any of them.
func TestNoWorkloadSelectsNodesByHostname(t *testing.T) {
	for _, located := range loadDirectoryManifests(t) {
		for _, selector := range hostnameNodeSelectors(located.workloadManifest) {
			t.Errorf("%s/%s selects nodes by hostname (%s); select on itsbagelbot.dev/role instead",
				located.filename, located.Metadata.Name, selector)
		}
	}
}

// locatedManifest keeps a decoded manifest next to the file it came from, so a
// failure can name the file without that filename being threaded through the
// call chain as a bare string argument.
type locatedManifest struct {
	workloadManifest
	filename string
}

// loadDirectoryManifests decodes every YAML document in this directory.
func loadDirectoryManifests(t *testing.T) []locatedManifest {
	t.Helper()

	var located []locatedManifest
	for _, filename := range manifestFilenames(t) {
		f, err := os.Open(filename)
		if err != nil {
			t.Fatal(err)
		}
		for _, manifest := range decodeManifests(t, f) {
			located = append(located, locatedManifest{workloadManifest: manifest, filename: filename})
		}
		f.Close()
	}
	return located
}

func manifestFilenames(t *testing.T) []string {
	t.Helper()

	filenames, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(filenames) == 0 {
		t.Fatal("no manifests found; the glob is wrong")
	}
	return filenames
}

// pinningIsWorthIt lists the StatefulSets allowed to claim a PersistentVolume.
//
// A local-path PVC binds its PV to whichever node provisioned it, so a member
// whose node dies sits Pending on "didn't match PersistentVolume's node
// affinity" until someone deletes the claim. On 2026-07-27 that stranded nats-1
// on a dead node5, took JetStream below quorum, and crashlooped every service
// that opens a consumer at startup. Pinning is therefore opt-in and has to earn
// its place, which only nats does:
//
//   - nats: a cold start re-establishes every stream AND all 24+ consumer RAFT
//     groups from its peers. Measured that day, a restarted member sat "not
//     current" on 10-27 groups for minutes, surfacing as chat replies that were
//     slow or dropped past MaxDeliver. The cost scales with GROUP COUNT, not
//     data: the streams held under a thousand messages.
//
// Valkey is deliberately absent. Its master/replica topology rebuilds a member
// with one full resync of a small derived dataset, so an empty volume costs a
// few seconds rather than minutes of degraded consumer state.
var pinningIsWorthIt = map[string]string{
	"nats": "cold start rebuilds 24+ consumer RAFT groups; see the volumeClaimTemplates comment in nats.yaml",
}

// TestOnlyDocumentedStatefulSetsPinAVolume keeps the exception list honest.
//
// This has to be a test rather than a comment because volumeClaimTemplates is
// IMMUTABLE. Server-side apply will neither add nor remove one: dropping it
// leaves the claim template in place and adds the new volume alongside,
// producing a duplicate volume name while Flux still reports the reconcile as
// successful. Either direction needs a delete-and-recreate of the StatefulSet,
// so the decision must be caught in review rather than at deploy time.
func TestOnlyDocumentedStatefulSetsPinAVolume(t *testing.T) {
	for _, located := range loadDirectoryManifests(t) {
		if located.Kind != "StatefulSet" || len(located.Spec.VolumeClaimTemplates) == 0 {
			continue
		}
		if _, ok := pinningIsWorthIt[located.Metadata.Name]; ok {
			continue
		}
		for _, vct := range located.Spec.VolumeClaimTemplates {
			t.Errorf("%s/%s claims volume %q but is not in pinningIsWorthIt; a local-path PVC strands the pod when its node dies, so either justify it there or use an emptyDir and let replication rebuild the member",
				located.filename, located.Metadata.Name, vct.Metadata.Name)
		}
	}
}

// decodeManifests reads every YAML document from r through the workload shape.
// Documents that are not workloads (Services, ConfigMaps, CRs) simply decode to
// a zero value with no selector terms, so they cost nothing to carry and no
// shape filtering is needed here.
func decodeManifests(t *testing.T, r io.Reader) []workloadManifest {
	t.Helper()

	var manifests []workloadManifest
	decoder := yaml.NewDecoder(r)
	for {
		var manifest workloadManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			return manifests
		}
		manifests = append(manifests, manifest)
	}
}

// hostnameNodeSelectors reports each required nodeAffinity expression that
// selects on kubernetes.io/hostname, rendered for the failure message.
func hostnameNodeSelectors(manifest workloadManifest) []string {
	var selectors []string
	for _, term := range manifest.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms {
		for _, expression := range term.MatchExpressions {
			if expression.Key == "kubernetes.io/hostname" {
				selectors = append(selectors, fmt.Sprintf("%s %v", expression.Operator, expression.Values))
			}
		}
	}
	return selectors
}

func TestConsoleAdminExplicitlyExcludesWorkerPool(t *testing.T) {
	admin := loadDeployment(t, "console-admin.yaml", "console-admin")
	terms := admin.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms

	for _, term := range terms {
		for _, expression := range term.MatchExpressions {
			if excludesWorkerPool(expression.Key, expression.Operator, expression.Values) {
				return
			}
		}
	}

	t.Fatal("console-admin must explicitly exclude the worker pool")
}
