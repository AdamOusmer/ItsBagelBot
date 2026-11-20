// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitchauth_test

import (
	"slices"
	"testing"

	"ItsBagelBot/internal/domain/twitchauth"
)

func TestGap(t *testing.T) {
	required := twitchauth.Required()
	slices.Sort(required)

	full := slices.Clone(required)
	slices.Reverse(full)
	missing := []string{"bits:read", "user:read:email"}
	partial := slices.DeleteFunc(slices.Clone(full), func(scope string) bool {
		return slices.Contains(missing, scope)
	})

	tests := []struct {
		name    string
		granted []string
		want    []string
	}{
		{name: "nil grant", want: required},
		{name: "empty grant", granted: []string{}, want: required},
		{name: "openid only", granted: []string{"openid"}, want: required},
		{name: "missing scopes are sorted", granted: append(partial, "openid", "channel:bot"), want: missing},
		{name: "full grant in reverse order", granted: full},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := twitchauth.Gap(tc.granted); !slices.Equal(got, tc.want) {
				t.Fatalf("Gap(%v) = %v, want %v", tc.granted, got, tc.want)
			}
		})
	}
}

func TestSetVersionIsPinned(t *testing.T) {
	if got, want := twitchauth.SetVersion, "bb64e312d9ada33e"; got != want {
		t.Fatalf("SetVersion = %q, want %q", got, want)
	}
}
