// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import "testing"

// A production boot that forgot APP_ENV used to get development logging
// (verbose, unsampled) with nothing saying so. The fallback must be
// production, and it must be visible: defaulted=true is what NewCore turns
// into the one-time boot warning.
func TestResolveAppEnvDefaultsToProductionWhenUnset(t *testing.T) {
	scenarios := []struct {
		name       string
		value      string
		wantEnv    string
		wantNotice bool
	}{
		{"unset", "", appEnvProduction, true},
		{"empty counts as unset", "", appEnvProduction, true},
		{"explicit production", "production", "production", false},
		{"explicit development", "development", "development", false},
		{"explicit debug", "debug", "debug", false},
	}
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{"APP_ENV": tc.value}
			get := func(key string) string { return values[key] }

			env, defaulted := resolveAppEnv(get)
			if env != tc.wantEnv {
				t.Fatalf("env = %q, want %q", env, tc.wantEnv)
			}
			if defaulted != tc.wantNotice {
				t.Fatalf("defaulted = %v, want %v", defaulted, tc.wantNotice)
			}
		})
	}
}
