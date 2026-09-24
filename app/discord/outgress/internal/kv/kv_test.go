// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kv

import "testing"

func TestNewWithNilClientReturnsNilStore(t *testing.T) {
	if s := New(nil); s != nil {
		t.Fatalf("New(nil) = %v, want nil", s)
	}
}
