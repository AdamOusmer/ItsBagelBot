// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"
)

func TestBuildKeysRoundTripsThroughYAML(t *testing.T) {
	acl := smallTestACL()
	mat := testMaterialFor(t, acl)
	activations, err := mintActivations(acl, mat, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := buildKeys(mat, activations)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "accounts.keys.yaml")
	if err := writeKeysYAML(path, keys); err != nil {
		t.Fatal(err)
	}
	loaded, err := natsacl.LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Operator != keys.Operator {
		t.Fatalf("Operator = %s, want %s", loaded.Operator, keys.Operator)
	}
	if loaded.Roles["BUS"]["outgress_bus"] != keys.Roles["BUS"]["outgress_bus"] {
		t.Fatal("role key did not round-trip")
	}
}

func TestWriteOperatorJWTWritesTheEncodedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operator.jwt")
	if err := writeOperatorJWT(path, "header.payload.sig"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "header.payload.sig\n" {
		t.Fatalf("file content = %q", got)
	}
}

func mustWriteIfChanged(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := writeIfChanged(path, content); err != nil {
		t.Fatal(err)
	}
}

func mustModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func TestWriteIfChangedSkipsWhenContentMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	mustWriteIfChanged(t, path, []byte("hello"))
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	mustWriteIfChanged(t, path, []byte("hello"))
	if !mustModTime(t, path).Equal(old) {
		t.Fatal("writeIfChanged rewrote identical content, changing mtime")
	}

	mustWriteIfChanged(t, path, []byte("world"))
	if mustModTime(t, path).Equal(old) {
		t.Fatal("writeIfChanged did not rewrite changed content")
	}
}
