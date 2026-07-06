package ratelimit

import (
	"sort"

	"github.com/cespare/xxhash/v2"
)

func membershipGeneration(members []Member) uint64 {
	ordered := append([]Member(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PodID < ordered[j].PodID })
	hash := xxhash.New()
	separator := [1]byte{0}
	for _, member := range ordered {
		_, _ = hash.WriteString(member.PodID)
		_, _ = hash.Write(separator[:])
		_, _ = hash.WriteString(member.Region)
		_, _ = hash.Write(separator[:])
	}
	generation := hash.Sum64()
	if generation == 0 {
		generation = 1
	}
	return generation
}

func BuildPlan(epoch uint64, validFromMS, validUntilMS int64, members []Member) (*Plan, error) {
	ordered := append([]Member(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PodID < ordered[j].PodID })
	plan := &Plan{
		Version: planVersion, Epoch: epoch, Generation: membershipGeneration(ordered),
		ValidFromMS: validFromMS, ValidUntilMS: validUntilMS, Members: ordered,
	}
	if err := plan.ComputeDigest(); err != nil {
		return nil, err
	}
	return plan, nil
}
