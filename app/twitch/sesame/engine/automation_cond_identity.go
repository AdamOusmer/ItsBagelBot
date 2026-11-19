// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/automation"
)

func (p *Pipeline) conditionPerm(_ context.Context, run *AutomationRun, c automation.PermCondition) (bool, error) {
	return runRole(run).Allows(module.ParsePerm(c.Level)), nil
}

func runRole(run *AutomationRun) module.Role {
	switch {
	case run.Ctx != nil:
		return run.Ctx.Chatter()
	case run.Env == nil:
		return module.RoleBroadcaster
	default:
		return run.DetachedRole
	}
}

func (p *Pipeline) conditionUsers(_ context.Context, run *AutomationRun, c automation.UsersCondition) (bool, error) {
	return userPasses(&run.Viewer, &c), nil
}

func userPasses(v *Viewer, c *automation.UsersCondition) bool {
	switch {
	case userListed(v, c.Deny, c.DenyUserIDs):
		return false
	case len(c.Allow) == 0 && len(c.AllowIDs) == 0:
		return true
	}
	return userListed(v, c.Allow, c.AllowUserIDs)
}

func userListed(v *Viewer, logins []string, ids []uint64) bool {
	return containsFold(logins, v.Login) || (v.ID != 0 && slices.Contains(ids, v.ID))
}

func containsFold(list []string, value string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, value) })
}

func (p *Pipeline) conditionCategory(ctx context.Context, run *AutomationRun, c automation.CategoryCondition) (bool, error) {
	if p.streamInfo == nil {
		return false, nil
	}
	res, err := p.streamInfo.Lookup(ctx, run.Channel, "")
	if err != nil {
		return false, err
	}
	return listPasses(res.GameName, c.Allow, c.Deny), nil
}

func listPasses(value string, allow, deny []string) bool {
	return !containsFold(deny, value) && (len(allow) == 0 || containsFold(allow, value))
}

func (p *Pipeline) conditionLive(ctx context.Context, run *AutomationRun, c automation.LiveCondition) (bool, error) {
	if p.live == nil {
		return false, nil
	}
	live, err := p.live.IsLive(ctx, run.BroadcasterID)
	if err != nil {
		return false, err
	}
	return live == (c.State == "live"), nil
}

func (p *Pipeline) conditionFollow(ctx context.Context, run *AutomationRun, c automation.FollowCondition) (bool, error) {
	if run.Viewer.ID == 0 || p.followage == nil {
		return false, nil
	}
	result, err := p.followage.Lookup(ctx, FollowageQuery{
		BroadcasterID: strconv.FormatUint(run.BroadcasterID, 10),
		TargetID:      strconv.FormatUint(run.Viewer.ID, 10),
		Trial:         run.Env != nil && run.Env.Origin == "trial",
	})
	if err != nil {
		return false, err
	}
	return result.Following && meetsAgeDays(result.FollowedAt, c.MinDays), nil
}

func (p *Pipeline) conditionAccountAge(ctx context.Context, run *AutomationRun, c automation.AccountAgeCondition) (bool, error) {
	if run.Viewer.ID == 0 || p.accountAge == nil {
		return false, nil
	}
	result, err := p.accountAge.Lookup(ctx, strconv.FormatUint(run.Viewer.ID, 10), "")
	if err != nil {
		return false, err
	}
	return result.UserFound && meetsAgeDays(result.CreatedAt, c.MinDays), nil
}

func meetsAgeDays(at time.Time, days int) bool {
	return !at.IsZero() && time.Since(at) >= time.Duration(days)*24*time.Hour
}
