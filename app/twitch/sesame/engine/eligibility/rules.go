// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import "ItsBagelBot/pkg/codec"

type Role string

const (
	Everyone Role = "everyone"
	Sub      Role = "sub"
	VIP      Role = "vip"
)

type Rules struct {
	Role            Role   `json:"role,omitempty"`
	ExcludeMods     bool   `json:"exclude_mods,omitempty"`
	MinPoints       int64  `json:"min_points,omitempty"`
	MinWatchSeconds uint64 `json:"min_watch_seconds,omitempty"`
	FollowersOnly   bool   `json:"followers_only,omitempty"`
	MinFollowDays   int    `json:"min_follow_days,omitempty"`
	MinAccountDays  int    `json:"min_account_days,omitempty"`
	SkipWinnersDays int    `json:"skip_winners_days,omitempty"`
	SubLuckPercent  int    `json:"sub_luck_percent,omitempty"`
}

func Parse(raw codec.RawMessage) (Rules, error) {
	var rules Rules
	if len(raw) == 0 {
		return rules, nil
	}
	if err := codec.Unmarshal(raw, &rules); err != nil {
		return Rules{}, err
	}
	return rules.Clamp(), nil
}

func (r Rules) Clamp() Rules {
	r.Role = validRole(r.Role)
	r.MinPoints = min(max(r.MinPoints, 0), Limits.MaxPoints)
	r.MinWatchSeconds = min(r.MinWatchSeconds, Limits.MaxWatchHours*3600)
	r.MinFollowDays = min(max(r.MinFollowDays, 0), Limits.MaxFollowDays)
	r.MinAccountDays = min(max(r.MinAccountDays, 0), Limits.MaxAccountDays)
	r.SkipWinnersDays = min(max(r.SkipWinnersDays, 0), Limits.MaxSkipDays)
	if r.SubLuckPercent > 0 {
		r.SubLuckPercent = min(max(r.SubLuckPercent, Limits.LuckMin), Limits.LuckMax)
	}
	r.SubLuckPercent = max(r.SubLuckPercent, 0)
	return r
}

func validRole(role Role) Role {
	switch role {
	case Everyone, Sub, VIP:
		return role
	default:
		return ""
	}
}

func (r Rules) NeedsStandings() bool {
	return r.MinPoints > 0 || r.MinWatchSeconds > 0
}

func (r Rules) DrawRules() Rules {
	return Rules{FollowersOnly: r.FollowersOnly, MinFollowDays: r.MinFollowDays,
		MinAccountDays: r.MinAccountDays, SkipWinnersDays: r.SkipWinnersDays}
}

type Reason string

const (
	ReasonRole         Reason = "role"
	ReasonBroadcaster  Reason = "broadcaster"
	ReasonMod          Reason = "mod"
	ReasonPoints       Reason = "points"
	ReasonWatch        Reason = "watch"
	ReasonFollow       Reason = "follow"
	ReasonScope        Reason = "scope"
	ReasonFollowAge    Reason = "follow_age"
	ReasonAccountAge   Reason = "account_age"
	ReasonRecentWinner Reason = "recent_winner"
	ReasonLoyaltyOff   Reason = "loyalty_off"
	ReasonStalled      Reason = "stalled"
)

func OpenGate(r Rules, loyaltyOn bool, state string) (bool, Reason) {
	switch {
	case !r.NeedsStandings():
		return true, ""
	case !loyaltyOn:
		return false, ReasonLoyaltyOff
	case state == "stalled":
		return false, ReasonStalled
	default:
		return true, ""
	}
}
