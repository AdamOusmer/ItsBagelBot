// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey_test

import (
	"testing"

	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/stretchr/testify/require"
)

func TestWindowHealthTracksOnlyCommittedPages(t *testing.T) {
	client := valkeytest.Spawn(t)
	ctx := t.Context()
	const state, due, claim, live = "test:window", "test:due", "test:claim", "test:live"
	for _, tc := range []struct {
		name, complete string
		wantDue        int64
	}{{"partial page", "0", 1000}, {"final page", "1", 301000}} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, pkgvalkey.DelKeys(ctx, client, state, due, claim, live))
			require.NoError(t, pkgvalkey.NewKV(client).Set(ctx, pkgvalkey.Key{Name: live}, "1"))
			type windowStep struct {
				script     pkgvalkey.Script
				keys, args []string
				result     int64
				want       pkgvalkey.WindowHealth
			}
			steps := []windowStep{
				{pkgvalkey.ArmWindow, []string{state, due, claim, live}, []string{"42", "1", "a", "1", "1000", "online"}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: 1000}},
				{pkgvalkey.ClaimWindow, []string{state, due, claim}, []string{"42", "owner", "1000", "5000"}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: 1000}},
				{pkgvalkey.ConfirmWindow, []string{state, claim, due}, []string{"owner", "1000", "stream-one", "1", "42", "301000"}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: 1000}},
				{pkgvalkey.SaveWindowPage, []string{state, claim}, []string{"owner", "42:a:1:1000", "payload", "next", tc.complete}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: 1000}},
				{pkgvalkey.RetryWindow, []string{state, due, claim}, []string{"owner", "42", "1050", "1050", "fetch failed", "60000", "300000", "2"}, 1, pkgvalkey.WindowHealth{Active: true, Failed: true, DueAt: 1000}},
				{pkgvalkey.CommitWindowPage, []string{state, due, claim}, []string{"stale", "42", "42:a:1:1000", "payload", tc.complete, "1100", "300000"}, 0, pkgvalkey.WindowHealth{Active: true, Failed: true, DueAt: 1000}},
				{pkgvalkey.CommitWindowPage, []string{state, due, claim}, []string{"owner", "42", "42:a:1:1000", "payload", tc.complete, "1200", "300000"}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: tc.wantDue, LastPageAt: 1200}},
			}
			reset := windowStep{pkgvalkey.ArmWindow, []string{state, due, claim, live}, []string{"42", "1", "b", "1", "4000", "online"}, 1, pkgvalkey.WindowHealth{Active: true, DueAt: 4000}}
			if tc.complete == "0" {
				reset = windowStep{pkgvalkey.ConfirmWindow, []string{state, claim, due}, []string{"owner", "1300", "stream-two", "1200", "42", "301300"}, 2, pkgvalkey.WindowHealth{Active: true, DueAt: 301300}}
			}
			steps = append(steps, reset)
			for _, step := range steps {
				result, err := step.script.Exec(ctx, client, step.keys, step.args).AsInt64()
				require.NoError(t, err)
				require.Equal(t, step.result, result)

				got, err := pkgvalkey.ReadWindowHealth(ctx, client, state)
				require.NoError(t, err)
				require.Equal(t, step.want, got)
			}
		})
	}
}
