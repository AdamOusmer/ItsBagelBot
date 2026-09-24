// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package buildguard

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func deps(t *testing.T, pkg string) []string {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	out, err := exec.Command(goBin, "list", "-deps", pkg).CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
	}
	return strings.Fields(string(out))
}

func TestSesameIsReadOnlyToData(t *testing.T) {
	for _, dep := range deps(t, "ItsBagelBot/app/twitch/sesame") {
		if isDataAccessPackage(dep) {
			t.Fatalf("app/twitch/sesame must not depend on a DB/ent package, but links %q; sesame is read-only to the projection (Valkey + projector RPC)", dep)
		}
	}
}

func isDataAccessPackage(dep string) bool {
	isEnt := strings.HasPrefix(dep, "ItsBagelBot/") && (strings.HasSuffix(dep, "/ent") || strings.Contains(dep, "/ent/"))
	isDriver := strings.HasPrefix(dep, "github.com/go-sql-driver/") || strings.Contains(dep, "/nrmysql")
	return isEnt || isDriver || dep == "ItsBagelBot/pkg/db"
}

func TestEngineDoesNotImportModules(t *testing.T) {
	const modules = "ItsBagelBot/app/twitch/sesame/modules"
	for _, dep := range deps(t, "ItsBagelBot/app/twitch/sesame/engine") {
		if dep == modules {
			t.Fatalf("app/twitch/sesame/engine must not import %q; the engine depends on module abstractions only, main wires the concrete modules", modules)
		}
	}
}
