// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"io/fs"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
)

type kindSet map[string]bool

func readTree(t *testing.T, fsys fs.FS, dir ports.FilePath) ports.Files {
	t.Helper()
	files := ports.Files{}
	err := fs.WalkDir(fsys, string(dir), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		files[ports.FilePath(p)] = b
		return err
	})
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return files
}

func buildDir(t *testing.T, fsys fs.FS, root ports.FilePath) ports.Objects {
	t.Helper()
	objs, err := build(ports.BuildSpec{Files: readTree(t, fsys, root), Root: root})
	if err != nil {
		t.Fatalf("build %s: %v", root, err)
	}
	return objs
}

func testdataObjects(t *testing.T) ports.Objects {
	t.Helper()
	return buildDir(t, os.DirFS("."), "testdata/k8s")
}

func refs(objs ports.Objects, kinds kindSet) []string {
	out := []string{}
	for _, o := range objs {
		if len(kinds) == 0 || kinds[o.GetKind()] {
			out = append(out, refString(refOf(o)))
		}
	}
	return out
}

func clone(objs ports.Objects) ports.Objects {
	out := make(ports.Objects, len(objs))
	for i, o := range objs {
		out[i] = o.DeepCopy()
	}
	return out
}

func find(t *testing.T, objs ports.Objects, ref ports.ObjectRef) *unstructured.Unstructured {
	t.Helper()
	for _, o := range objs {
		if refOf(o) == ref {
			return o
		}
	}
	t.Fatalf("no %s in fixture", refString(ref))
	return nil
}

type manifest struct {
	apiVersion string
	kind       string
	ns         ports.Namespace
	name       string
	spec       map[string]any
	data       map[string]any
}

func (m manifest) obj() *unstructured.Unstructured {
	o := &unstructured.Unstructured{Object: map[string]any{}}
	if m.spec != nil {
		o.Object["spec"] = m.spec
	}
	if m.data != nil {
		o.Object["data"] = m.data
	}
	o.SetAPIVersion(m.apiVersion)
	o.SetKind(m.kind)
	o.SetNamespace(string(m.ns))
	o.SetName(m.name)
	return o
}

func objects(ms ...manifest) ports.Objects {
	out := make(ports.Objects, len(ms))
	for i, m := range ms {
		out[i] = m.obj()
	}
	return out
}
