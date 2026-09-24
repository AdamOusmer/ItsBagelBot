// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/natsacl"
)

// testMaterialFor mints every seed a small ACL fixture needs, in memory,
// with no Doppler involved.
func testMaterialFor(t *testing.T, acl *natsacl.ACL) *material {
	t.Helper()
	mat := newMaterial(acl)
	for _, task := range buildKeyTasks(acl, mat) {
		kp, err := task.create()
		if err != nil {
			t.Fatal(err)
		}
		task.assign(kp)
	}
	return mat
}

func mustMint(t *testing.T, created bool, err error) {
	t.Helper()
	if err != nil || !created {
		t.Fatalf("created=%v err=%v, want a new credential", created, err)
	}
}

func mustSkip(t *testing.T, created bool, err error) {
	t.Helper()
	if err != nil || created {
		t.Fatalf("created=%v err=%v, want no-op", created, err)
	}
}
