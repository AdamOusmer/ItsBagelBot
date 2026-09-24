// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import (
	"reflect"
	"sort"

	"github.com/nats-io/jwt/v2"
)

// Equivalent ignores signing metadata that differs on every re-sign of
// identical content (IssuedAt, ID, Issuer).
func Equivalent(a, b *jwt.AccountClaims) bool {
	if a == nil || b == nil {
		return a == b
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

func normalize(a *jwt.AccountClaims) *jwt.AccountClaims {
	c := *a
	c.ID, c.IssuedAt, c.Issuer = "", 0, ""

	exports := append(jwt.Exports{}, c.Exports...)
	sort.Sort(exports)
	c.Exports = exports

	imports := append(jwt.Imports{}, c.Imports...)
	sort.Sort(imports)
	c.Imports = imports

	return &c
}
