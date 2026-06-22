// Package buildguard holds build-level regression guards that ordinary unit
// tests cannot catch. Its only test asserts every data service binary links in
// its ent runtime.
package buildguard

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// dataServices construct an ent client in main and therefore MUST blank-import
// their generated ent/runtime, or every write fails at runtime with
// "ent: uninitialized ... (forgotten import ent/runtime?)". That import is a
// side-effect-only dependency, so nothing at compile time forces it and a
// refactor can silently drop it (unit tests pass because enttest pulls it in).
// This test fails the build instead, by checking the real dependency graph of
// each service's main package.
var dataServices = []string{"commands", "modules", "users", "transactions"}

func TestDataServicesLinkEntRuntime(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")

	for _, svc := range dataServices {
		svc := svc
		t.Run(svc, func(t *testing.T) {
			pkg := "ItsBagelBot/app/" + svc
			want := pkg + "/ent/runtime"

			out, err := exec.Command(goBin, "list", "-deps", pkg).CombinedOutput()
			if err != nil {
				t.Fatalf("go list -deps %s: %v\n%s", pkg, err, out)
			}

			for _, dep := range strings.Fields(string(out)) {
				if dep == want {
					return
				}
			}
			t.Fatalf("%s main does not import %q; add `_ %q` so ent field defaults/hooks initialize and writes persist", pkg, want, want)
		})
	}
}
