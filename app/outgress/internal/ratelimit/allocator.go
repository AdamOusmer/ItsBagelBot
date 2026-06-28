package ratelimit

import (
	"crypto/sha256"
	"encoding/binary"
)

// RendezvousHash returns a score for (key, epoch, member).
func RendezvousHash(key string, epoch uint64, memberID string) uint64 {
	h := sha256.New()
	h.Write([]byte(key))
	
	var epochBytes [8]byte
	binary.LittleEndian.PutUint64(epochBytes[:], epoch)
	h.Write(epochBytes[:])
	
	h.Write([]byte(memberID))
	
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8])
}

// AssignChatOwner uses rendezvous hashing to pick one owner.
func AssignChatOwner(bucket string, epoch uint64, members []Member) string {
	if len(members) == 0 {
		return ""
	}
	bestMember := members[0].PodID
	var maxScore uint64

	for _, m := range members {
		score := RendezvousHash(bucket, epoch, m.PodID)
		if score > maxScore || (score == maxScore && m.PodID > bestMember) {
			maxScore = score
			bestMember = m.PodID
		}
	}
	return bestMember
}

// BuildPlan creates a new plan for the epoch.
// For shadow mode, this just implements the chat bucket layout logic.
func BuildPlan(epoch uint64, validFromMS, validUntilMS int64, members []Member, chatBuckets []string) (*Plan, error) {
	plan := &Plan{
		Version:      1,
		Epoch:        epoch,
		ValidFromMS:  validFromMS,
		ValidUntilMS: validUntilMS,
		Members:      members,
	}

	for _, bucket := range chatBuckets {
		owner := AssignChatOwner(bucket, epoch, members)
		plan.Allocations = append(plan.Allocations, Allocation{
			Bucket:     bucket,
			Holder:     owner,
			Generation: epoch, // Using epoch as generation for simplicity in shadow mode
			RateMicros: int64(20.0 / 30.0 * 1000000 * 0.9), // 90% leased (20 per 30s)
			Burst:      int(20.0 * 0.9),
		})
	}
	
	// Helix allocations would be added similarly

	if err := plan.ComputeDigest(); err != nil {
		return nil, err
	}
	return plan, nil
}
