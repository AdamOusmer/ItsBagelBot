// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"testing"

	"github.com/nats-io/jwt/v2"
)

func TestEquivalentIgnoresSigningMetadata(t *testing.T) {
	claims, _, _ := compileFixture(t)
	a := byName(claims, "EXPORTER")

	resigned := *a
	resigned.ID = "different-jti"
	resigned.IssuedAt = 1234
	resigned.Issuer = "OACCOUNT"

	if !Equivalent(a, &resigned) {
		t.Fatal("re-signing metadata should not change equivalence")
	}
}

func TestEquivalentIgnoresExportOrder(t *testing.T) {
	claims, _, _ := compileFixture(t)
	a := byName(claims, "EXPORTER")

	reordered := *a
	reordered.Exports = jwt.Exports{a.Exports[2], a.Exports[0], a.Exports[1]}

	if !Equivalent(a, &reordered) {
		t.Fatal("export order should not change equivalence")
	}
}

func TestEquivalentDetectsChangedGrant(t *testing.T) {
	claims, _, _ := compileFixture(t)
	a := byName(claims, "EXPORTER")

	changed := *a
	dropped := append(jwt.Exports{}, a.Exports[:len(a.Exports)-1]...)
	changed.Exports = dropped

	if Equivalent(a, &changed) {
		t.Fatal("dropping a grant must be detected as a difference")
	}
}

func TestEquivalentNilHandling(t *testing.T) {
	claims, _, _ := compileFixture(t)
	a := byName(claims, "EXPORTER")

	if Equivalent(a, nil) {
		t.Fatal("nil must never be equivalent to a real claim")
	}
	if !Equivalent(nil, nil) {
		t.Fatal("nil should be equivalent to nil")
	}
}
