package ratelimit

import (
	"reflect"
	"testing"
)

func TestPlanValidationDoesNotMutate(t *testing.T) {
	plan, err := BuildPlan(1, 1, 2, []Member{
		{PodID: "b", Region: "west"},
		{PodID: "a", Region: "east"},
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeMembers := append([]Member(nil), plan.Members...)
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeMembers, plan.Members) {
		t.Fatal("validation mutated plan slices")
	}
	plan.ValidUntilMS++
	if plan.Validate() == nil {
		t.Fatal("tampered plan passed digest validation")
	}
}
