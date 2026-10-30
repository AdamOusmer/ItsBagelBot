// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package deploy

import (
	"reflect"
	"testing"
)

func TestStagesForBracketsEveryKind(t *testing.T) {
	for _, kind := range []RunKind{KindRelease, KindHotfix, KindBump, KindRollback, KindReapply} {
		got := StagesFor(kind)
		ends := []StageID{got[0], got[len(got)-1]}
		if want := []StageID{StagePreflight, StagePromote}; !reflect.DeepEqual(ends, want) {
			t.Errorf("%s: ends %v, want %v", kind, ends, want)
		}
	}
	if StagesFor("nope") != nil {
		t.Error("unknown kind must have no stages")
	}
}

func TestStagesForReturnsCopy(t *testing.T) {
	StagesFor(KindRelease)[0] = StagePromote
	if StagesFor(KindRelease)[0] != StagePreflight {
		t.Error("caller mutated the shared table")
	}
}

func TestCurrentStageSkipsFinished(t *testing.T) {
	run := Run{Stages: []Stage{
		{ID: StagePreflight, State: StateSucceeded},
		{ID: StageMergePRs, State: StateSkipped},
		{ID: StageBuild, State: StateFailed},
	}}
	if got := run.CurrentStage(); got != StageBuild {
		t.Errorf("current stage %q, want %q", got, StageBuild)
	}
}
