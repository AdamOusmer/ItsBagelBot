// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

type Regress int

const (
	RegressStandard Regress = iota
	RegressPremium
	RegressStream
)

func (r Regress) String() string {
	switch r {
	case RegressPremium:
		return "premium"
	case RegressStream:
		return "stream"
	default:
		return "standard"
	}
}

func RegressFromLane(lane string) Regress {
	switch lane {
	case "premium":
		return RegressPremium
	case "stream":
		return RegressStream
	default:
		return RegressStandard
	}
}

func (r Regress) IsPremium() bool { return r == RegressPremium }
