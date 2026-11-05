// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"context"
	"math/rand/v2"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/automation"
)

var compareOps = map[string]func(int) bool{
	"eq":  func(c int) bool { return c == 0 },
	"ne":  func(c int) bool { return c != 0 },
	"gt":  func(c int) bool { return c > 0 },
	"gte": func(c int) bool { return c >= 0 },
	"lt":  func(c int) bool { return c < 0 },
	"lte": func(c int) bool { return c <= 0 },
}

func compareValues[T cmp.Ordered](op string, a, b T) bool {
	fn := compareOps[op]
	return fn != nil && fn(cmp.Compare(a, b))
}

func (p *Pipeline) automationIntN(n int) int {
	if p.autoIntN != nil {
		return p.autoIntN(n)
	}
	return rand.IntN(n)
}

func (p *Pipeline) conditionChance(_ context.Context, _ *AutomationRun, c automation.ChanceCondition) (bool, error) {
	return p.automationIntN(100) < c.Percent, nil
}

func (p *Pipeline) conditionCounter(ctx context.Context, run *AutomationRun, c automation.CounterCondition) (bool, error) {
	if p.loyalty == nil {
		return false, nil
	}
	counter, _, err := p.loyalty.CounterPeek(ctx, CounterTarget{BroadcasterID: run.BroadcasterID, Name: c.Name, ViewerID: run.Viewer.ID})
	if err != nil {
		return false, err
	}
	return compareValues(c.Op, counter.Value, c.Value), nil
}

func (p *Pipeline) conditionPoints(ctx context.Context, run *AutomationRun, c automation.PointsCondition) (bool, error) {
	if p.loyalty == nil || run.Viewer.ID == 0 {
		return false, nil
	}
	balance, err := p.loyalty.BalanceGet(ctx, run.BroadcasterID, run.Viewer.ID)
	if err != nil {
		return false, err
	}
	return compareValues(c.Op, balance.Points, c.Value), nil
}

func (p *Pipeline) conditionArgRegex(_ context.Context, run *AutomationRun, c automation.ArgRegexCondition) (bool, error) {
	return c.Compiled != nil && c.Compiled.MatchString(run.Args), nil
}

func (p *Pipeline) conditionCooldown(ctx context.Context, run *AutomationRun, c automation.CooldownCondition) (bool, error) {
	if p.cooldown == nil {
		return false, nil
	}
	var windows []CooldownWindow
	bid := strconv.FormatUint(run.BroadcasterID, 10)
	if c.GlobalS > 0 {
		windows = append(windows, CooldownWindow{Key: "cooldown:auto:" + bid + ":" + run.Plan.Automation.ID, TTL: time.Duration(c.GlobalS) * time.Second})
	}
	if c.UserS > 0 {
		key := "cooldown:autouser:" + bid + ":" + strconv.FormatUint(run.Viewer.ID, 10) + ":" + run.Plan.Automation.ID
		windows = append(windows, CooldownWindow{Key: key, TTL: time.Duration(c.UserS) * time.Second})
	}
	return p.claimCooldownWindows(ctx, windows)
}

func (p *Pipeline) claimCooldownWindows(ctx context.Context, windows []CooldownWindow) (bool, error) {
	switch len(windows) {
	case 0:
		return true, nil
	case 1:
		return p.cooldown.Allow(ctx, windows[0].Key, windows[0].TTL)
	default:
		return p.cooldown.AllowAll(ctx, windows)
	}
}
