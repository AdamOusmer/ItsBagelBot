package module

// Regress is the lane an event arrived on. It is the worker's "regress status":
// it records whether ingress decided this traffic was premium or standard, so
// the pipeline can keep the same priority all the way out to outgress without
// re-deciding it. Stream is the live lane and carries no priority of its own.
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

// RegressFromLane maps the ingress lane string onto the regress status.
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

// IsPremium reports whether traffic on this regress should ride the premium lane.
func (r Regress) IsPremium() bool { return r == RegressPremium }
