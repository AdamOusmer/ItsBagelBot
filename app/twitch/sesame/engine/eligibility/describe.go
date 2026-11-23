// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

type Requirement struct {
	Key   string
	Value int64
}

type requirementItem struct {
	req Requirement
	on  bool
}

func Describe(r Rules) []Requirement {
	r = r.Clamp()
	items := []requirementItem{
		{Requirement{Key: "req.sub"}, r.Role == Sub},
		{Requirement{Key: "req.vip"}, r.Role == VIP},
		{Requirement{Key: "req.exclude_mods"}, r.ExcludeMods},
		{Requirement{"req.points", r.MinPoints}, r.MinPoints > 0},
		{Requirement{"req.watch_hours", int64(r.MinWatchSeconds)}, r.MinWatchSeconds > 0},
		{Requirement{"req.follow_days", int64(r.MinFollowDays)}, r.MinFollowDays > 0},
		{Requirement{Key: "req.followers"}, r.FollowersOnly && r.MinFollowDays == 0},
		{Requirement{"req.account_days", int64(r.MinAccountDays)}, r.MinAccountDays > 0},
		{Requirement{"req.skip_winners_days", int64(r.SkipWinnersDays)}, r.SkipWinnersDays > 0},
		{Requirement{"req.sub_luck", int64(r.SubLuckPercent)}, r.SubLuckPercent > Limits.LuckMin},
	}
	var reqs []Requirement
	for _, item := range items {
		if item.on {
			reqs = append(reqs, item.req)
		}
	}
	return reqs
}
