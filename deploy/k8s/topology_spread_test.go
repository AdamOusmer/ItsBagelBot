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

func TestRolloutSensitiveWorkloadsHardSpreadEachReplicaSet(t *testing.T) {
	tests := []struct {
		file       string
		name       string
		minDomains int
	}{
		{"console-admin.yaml", "console-admin", 0},
		// minDomains tracks the number of schedulable app-tier nodes, which is 3.
		// It must NEVER exceed that count: when it does, skew is measured against
		// a global minimum of zero and every replica becomes unschedulable, so an
		// over-large value is a total outage rather than a loose constraint. This
		// was 4 while the fleet had four app nodes, and only kept working after
		// the move because the cordoned old nodes still counted as domains
		// (nodeTaintsPolicy defaults to Ignore). Deleting them would have taken
		// both of these services to zero replicas.
		{"notifications.yaml", "notifications", 3},
		{"transactions.yaml", "transactions", 3},
		{"twitch-ingress.yaml", "twitch-ingress", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraint := hostnameSpread(t, loadDeployment(t, tt.file, tt.name))
			if constraint.WhenUnsatisfiable != "DoNotSchedule" {
				t.Fatalf("hostname spread is %q, want DoNotSchedule", constraint.WhenUnsatisfiable)
			}
			if !slices.Contains(constraint.MatchLabelKeys, "pod-template-hash") {
				t.Fatal("hostname spread must be scoped to the incoming ReplicaSet")
			}
			if constraint.MinDomains != tt.minDomains {
				t.Fatalf("minDomains = %d, want %d", constraint.MinDomains, tt.minDomains)
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

// TestNoStatefulSetClaimsAPersistentVolume guards the failure that took
// production down on 2026-07-27.
//
// A local-path PVC binds its PV to whichever node provisioned it. When node5
// died, nats-1 was pinned there and could not be rescheduled anywhere: it sat
// Pending on "didn't match PersistentVolume's node affinity" indefinitely, took
// JetStream below quorum, and crashlooped every service that opens a consumer at
// startup. One dead node became a full outage.
//
// Replication is the durability mechanism here, not the disk. Every stream is
// R3, so a member that restarts empty re-syncs from its peers, which is exactly
// how nats-1 was recovered onto node3.
//
// This has to be a test rather than a comment because volumeClaimTemplates is
// IMMUTABLE. Server-side apply will not remove one: it keeps the claim template
// and adds the emptyDir alongside it, producing a duplicate volume name while
// Flux still reports the reconcile as successful. Reintroducing one would need a
// delete-and-recreate of the StatefulSet to undo, so it must be caught in review.
func TestNoStatefulSetClaimsAPersistentVolume(t *testing.T) {
	for _, located := range loadDirectoryManifests(t) {
		if located.Kind != "StatefulSet" || len(located.Spec.VolumeClaimTemplates) == 0 {
			continue
		}
		for _, vct := range located.Spec.VolumeClaimTemplates {
			t.Errorf("%s/%s declares volumeClaimTemplates %q; use an emptyDir volume and let replication provide durability",
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
