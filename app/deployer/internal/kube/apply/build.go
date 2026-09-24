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

// Must round-trip through JSON: resource.Map() yields YAML ints that unstructured.Nested* panics on.
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
	body, err := codec.Marshal(map[string][]string{"resources": resources})
	if err != nil {
		return err
	}
	return fsys.WriteFile("/"+path.Join(root, "kustomization.yaml"), body)
}

func manifestsUnder(files ports.Files, root string) []string {
	prefix := root + "/"
	var out []string
	for p := range files {
		rel, ok := strings.CutPrefix(path.Clean(string(p)), prefix)
		direct, _ := path.Match("*.yaml", rel)
		if ok && direct {
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out
}
