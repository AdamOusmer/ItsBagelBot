// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"ItsBagelBot/internal/domain/rpc/deploy"
)

func TestPromoteStage(t *testing.T) {
	cases := []struct {
		name              string
		kind              deploy.RunKind
		target, pin, head deploy.SHA
		offMain           bool
		done              bool
		code              deploy.FailureCode
		calls             []string
	}{
		{name: "promotes the merged pin commit", kind: deploy.KindBump, target: "c1", pin: "c2", head: "c1", calls: []string{"move deploy c2"}},
		{name: "reapply promotes the main target", kind: deploy.KindReapply, target: "c2", head: "c1", calls: []string{"move deploy c2"}},
		{name: "rollback promotes a new main pin commit", kind: deploy.KindRollback, target: "c1", pin: "c2", head: "c1", calls: []string{"move deploy c2"}},
		{name: "exact main commit is already promoted", kind: deploy.KindBump, pin: "c2", head: "c2", done: true},
		{name: "same tree at another commit still needs promotion", kind: deploy.KindBump, pin: "c2", head: "legacy", calls: []string{"move deploy c2"}},
		{name: "creates a missing deploy branch", kind: deploy.KindBump, pin: "c2", calls: []string{"move deploy c2"}},
		{name: "refuses an off-main target", kind: deploy.KindBump, pin: "other", offMain: true, code: deploy.FailGitHub},
		{name: "does not accept an already promoted off-main target", kind: deploy.KindBump, pin: "other", head: "other", offMain: true, code: deploy.FailGitHub},
		{name: "missing target fails before reading deploy", kind: deploy.KindBump, code: deploy.FailInternal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(tc.kind)
			run.TargetSHA, run.Outputs.PinSHA = tc.target, tc.pin
			f := newFixture(t, run)
			f.gh.commitMain("c1", nil)
			f.gh.commitMain("c2", nil)
			if tc.head != "" {
				f.gh.branches["deploy"] = tc.head
			}
			f.gh.offMain["other"] = tc.offMain
			done, err := f.runStage(t, deploy.StagePromote)
			assert.Equal(t, tc.done, done)
			assert.Equal(t, tc.code, outcome(t, err))
			assert.Equal(t, tc.calls, f.gh.calls)
		})
	}
}
