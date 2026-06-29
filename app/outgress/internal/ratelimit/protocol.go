package ratelimit

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// Version 3 collapses the rollout modes: the quota-lease protocol is the sole
// implementation, so the plan no longer carries a mode and members no longer
// advertise a requested mode. Every member owns an equal share; no message is
// routed by channel.
const planVersion uint16 = 3

type Member struct {
	PodID  string `json:"pod_id"`
	Region string `json:"region"`
}

type Plan struct {
	Version      uint16   `json:"version"`
	Epoch        uint64   `json:"epoch"`
	Generation   uint64   `json:"generation"`
	Digest       string   `json:"digest"`
	ValidFromMS  int64    `json:"valid_from_ms"`
	ValidUntilMS int64    `json:"valid_until_ms"`
	Members      []Member `json:"members"`
}

var errInvalidPlan = errors.New("ratelimit: invalid lease plan")

func canonicalPlan(p Plan) Plan {
	p.Digest = ""
	p.Members = append([]Member(nil), p.Members...)
	sort.Slice(p.Members, func(i, j int) bool {
		if p.Members[i].PodID != p.Members[j].PodID {
			return p.Members[i].PodID < p.Members[j].PodID
		}
		return p.Members[i].Region < p.Members[j].Region
	})
	return p
}

func digestPlan(p Plan) ([sha256.Size]byte, error) {
	data, err := json.Marshal(canonicalPlan(p))
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func (p *Plan) ComputeDigest() error {
	digest, err := digestPlan(*p)
	if err != nil {
		return err
	}
	p.Digest = hex.EncodeToString(digest[:])
	return nil
}

func (p Plan) Validate() error {
	if p.Version != planVersion || p.Epoch == 0 || p.Generation == 0 ||
		p.ValidFromMS >= p.ValidUntilMS || len(p.Members) == 0 || len(p.Digest) != sha256.Size*2 {
		return errInvalidPlan
	}
	members := make(map[string]struct{}, len(p.Members))
	for _, member := range p.Members {
		if member.PodID == "" || member.Region == "" {
			return errInvalidPlan
		}
		if _, exists := members[member.PodID]; exists {
			return errInvalidPlan
		}
		members[member.PodID] = struct{}{}
	}
	want, err := hex.DecodeString(p.Digest)
	if err != nil {
		return errInvalidPlan
	}
	got, err := digestPlan(p)
	if err != nil || subtle.ConstantTimeCompare(want, got[:]) != 1 {
		return errInvalidPlan
	}
	return nil
}

func (p Plan) IsValid() bool { return p.Validate() == nil }
