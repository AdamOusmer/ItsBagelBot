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

var dataServices = []string{"commands", "modules", "users", "transactions"}

func TestDataServicesLinkEntRuntime(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")

	for _, svc := range dataServices {
		svc := svc
		t.Run(svc, func(t *testing.T) {
			pkg := "ItsBagelBot/app/db/" + svc
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
