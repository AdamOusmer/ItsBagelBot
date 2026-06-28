package ratelimit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type Member struct {
	PodID  string `json:"pod_id"`
	Region string `json:"region"`
	Weight int    `json:"weight"`
}

type Allocation struct {
	Bucket     string `json:"bucket"`
	Holder     string `json:"holder"`
	Generation uint64 `json:"generation"`
	RateMicros int64  `json:"rate_micros_per_second"`
	Burst      int    `json:"burst"`
}

type Plan struct {
	Version      uint16       `json:"version"`
	Epoch        uint64       `json:"epoch"`
	Digest       string       `json:"digest"`
	ValidFromMS  int64        `json:"valid_from_ms"`
	ValidUntilMS int64        `json:"valid_until_ms"`
	Members      []Member     `json:"members"`
	Allocations  []Allocation `json:"allocations"`
}

// ComputeDigest sorts slices canonically, zeroes the digest field, and computes the SHA-256 digest of the JSON.
func (p *Plan) ComputeDigest() error {
	p.Digest = ""

	sort.Slice(p.Members, func(i, j int) bool {
		return p.Members[i].PodID < p.Members[j].PodID
	})

	sort.Slice(p.Allocations, func(i, j int) bool {
		return p.Allocations[i].Bucket < p.Allocations[j].Bucket
	})

	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	hash := sha256.Sum256(data)
	p.Digest = hex.EncodeToString(hash[:])
	return nil
}

// IsValid validates a plan's digest and contents.
func (p *Plan) IsValid() bool {
	if p.Version != 1 || p.Epoch == 0 || p.Digest == "" || p.ValidFromMS >= p.ValidUntilMS {
		return false
	}
	
	expectedDigest := p.Digest
	if err := p.ComputeDigest(); err != nil {
		return false
	}
	isValid := expectedDigest == p.Digest
	p.Digest = expectedDigest // Restore it
	return isValid
}
