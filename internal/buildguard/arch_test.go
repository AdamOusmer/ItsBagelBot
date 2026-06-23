package buildguard

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// deps returns the full transitive dependency set of pkg via `go list -deps`.
func deps(t *testing.T, pkg string) []string {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	out, err := exec.Command(goBin, "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	return strings.Fields(string(out))
}

// TestWorkerIsReadOnlyToData asserts the worker never links a database or an ent
// package. The worker is a read-only consumer of the projection (Valkey + the
// projector RPC); only the projector writes Valkey and only the data services
// own a DB. A refactor that pulls an ent client or a SQL driver into the worker
// would silently grant it a write path it must not have, so fail the build here.
func TestWorkerIsReadOnlyToData(t *testing.T) {
	for _, dep := range deps(t, "ItsBagelBot/app/worker") {
		isEnt := strings.HasPrefix(dep, "ItsBagelBot/") && (strings.HasSuffix(dep, "/ent") || strings.Contains(dep, "/ent/"))
		if isEnt || dep == "ItsBagelBot/pkg/db" || dep == "database/sql" {
			t.Fatalf("app/worker must not depend on a DB/ent package, but links %q; the worker is read-only to the projection (Valkey + projector RPC)", dep)
		}
	}
}

// TestPipelineDoesNotImportBuiltin enforces the DIP boundary: the pipeline
// depends only on the module abstractions (Registry, Module, the store
// interfaces), never the concrete builtin modules. main is the single
// composition root that wires concretes. If the pipeline starts importing
// builtin, adding a feature would force pipeline edits (OCP violation).
func TestPipelineDoesNotImportBuiltin(t *testing.T) {
	const builtin = "ItsBagelBot/app/worker/module/builtin"
	for _, dep := range deps(t, "ItsBagelBot/app/worker/pipeline") {
		if dep == builtin {
			t.Fatalf("app/worker/pipeline must not import %q; the pipeline depends on module abstractions only, main wires concretes", builtin)
		}
	}
}
