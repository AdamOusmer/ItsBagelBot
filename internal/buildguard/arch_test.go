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

// TestSesameIsReadOnlyToData asserts sesame never links a database or an ent
// package. sesame (the Twitch event worker) is a read-only consumer of the
// projection (Valkey + the projector RPC); only the projector writes Valkey and
// only the data services own a DB. A refactor that pulls an ent client or a SQL
// driver into sesame would silently grant it a write path it must not have, so
// fail the build here.
func TestSesameIsReadOnlyToData(t *testing.T) {
	for _, dep := range deps(t, "ItsBagelBot/app/sesame") {
		isEnt := strings.HasPrefix(dep, "ItsBagelBot/") && (strings.HasSuffix(dep, "/ent") || strings.Contains(dep, "/ent/"))
		if isEnt || dep == "ItsBagelBot/pkg/db" || dep == "database/sql" {
			t.Fatalf("app/sesame must not depend on a DB/ent package, but links %q; sesame is read-only to the projection (Valkey + projector RPC)", dep)
		}
	}
}

// TestEngineDoesNotImportModules enforces the DIP boundary: the engine (registry,
// pipeline, gate) depends only on the module abstractions (module.Module and the
// store interfaces), never the concrete feature package. modules.All is wired by
// main, the single composition root. If the engine starts importing the modules
// package, adding a feature would force engine edits (OCP violation).
func TestEngineDoesNotImportModules(t *testing.T) {
	const modules = "ItsBagelBot/app/sesame/modules"
	for _, dep := range deps(t, "ItsBagelBot/app/sesame/engine") {
		if dep == modules {
			t.Fatalf("app/sesame/engine must not import %q; the engine depends on module abstractions only, main wires the concrete modules", modules)
		}
	}
}
