// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/konfig"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/pkg/codec"
)

// build runs kustomize over spec.Files in memory, the same render as
// `kubectl apply -k <Root>`. The files come from the GitHub API at the pin
// merge commit, never from a working tree, so nothing on the deployer's disk
// can leak into a build. krusty's defaults keep LoadRestrictionsRootOnly and
// plugins (exec, Helm) disabled; output keeps manifest order (no legacy kind
// sort), which staged() and Services() rely on.
func build(spec ports.BuildSpec) (ports.Objects, error) {
	root := path.Clean(string(spec.Root))
	fsys := filesys.MakeFsInMemory()
	for p, b := range spec.Files {
		if err := fsys.WriteFile("/"+path.Clean(string(p)), b); err != nil {
			return nil, fmt.Errorf("stage %s: %w", p, err)
		}
	}
	if err := ensureKustomization(fsys, spec.Files, root); err != nil {
		return nil, err
	}
	rm, err := krusty.MakeKustomizer(krusty.MakeDefaultOptions()).Run(fsys, "/"+root)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %s: %w", root, err)
	}
	return toObjects(rm)
}

// toObjects round-trips each resource through JSON: resource.Map() hands back
// YAML-decoded ints, and apimachinery's DeepCopyJSONValue (behind every
// unstructured.Nested* helper) panics on anything but int64 and float64.
func toObjects(rm resmap.ResMap) (ports.Objects, error) {
	objs := make(ports.Objects, 0, rm.Size())
	for _, r := range rm.Resources() {
		b, err := r.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", r.CurId(), err)
		}
		o := &unstructured.Unstructured{}
		if err := o.UnmarshalJSON(b); err != nil {
			return nil, fmt.Errorf("decode %s: %w", r.CurId(), err)
		}
		objs = append(objs, o)
	}
	return objs, nil
}

// ensureKustomization gives a root without a kustomization file one that lists
// every *.yaml directly in it (deploy/db has no kustomization; status-routes.yaml
// is applied as a plain manifest), so both shapes go through the same renderer.
func ensureKustomization(fsys filesys.FileSystem, files ports.Files, root string) error {
	for _, name := range konfig.RecognizedKustomizationFileNames() {
		if _, ok := files[ports.FilePath(path.Join(root, name))]; ok {
			return nil
		}
	}
	resources := manifestsUnder(files, root)
	if len(resources) == 0 {
		return fmt.Errorf("%w: no kustomization or *.yaml under %s", ports.ErrInvalid, root)
	}
	// JSON is YAML, and encoding/json quotes any file name safely.
	body, err := codec.Marshal(map[string][]string{"resources": resources})
	if err != nil {
		return err
	}
	return fsys.WriteFile("/"+path.Join(root, "kustomization.yaml"), body)
}

// manifestsUnder lists the *.yaml files directly in root, relative to it,
// sorted. Subdirectories are left out, as `kubectl apply -f <dir>` without -R
// leaves them out: GitHub's tree listing is recursive, and a nested directory
// may carry its own kustomization that is not a plain manifest.
func manifestsUnder(files ports.Files, root string) []string {
	prefix := root + "/"
	var out []string
	for p := range files {
		rel, ok := strings.CutPrefix(path.Clean(string(p)), prefix)
		// "*" never matches "/", so this is also the direct-child test.
		direct, _ := path.Match("*.yaml", rel)
		if ok && direct {
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out
}
