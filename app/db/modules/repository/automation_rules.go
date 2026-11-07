// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"ItsBagelBot/app/db/modules/ent"
	entautomation "ItsBagelBot/app/db/modules/ent/automation"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/rpc"
	"ItsBagelBot/pkg/codec"
)

const actionRun = "run"

// codeTimerExists keeps one owner per named cooldown across a channel's recipes.
const codeTimerExists = "timer_exists"

type channelCapCheck int

const (
	capNone channelCapCheck = iota
	capCount
	capLocked
)

func capCheckFor(existing *ent.Automation, in AutomationInput) channelCapCheck {
	switch {
	case existing != nil:
		return capNone
	case in.lockChannelCap:
		return capLocked
	default:
		return capCount
	}
}

func checkAutomationRules(ctx context.Context, client *ent.AutomationClient, draft automationDraft, capCheck channelCapCheck) error {
	var issues []automation.Issue
	if capCheck != capNone {
		full, err := channelAtCap(ctx, client, draft.userID, capCheck)
		if err != nil {
			return err
		}
		if full {
			issues = append(issues, automation.Issue{Path: "", Code: "too_many"})
		}
	}
	dups, err := duplicateIssues(ctx, client, draft)
	if err != nil {
		return err
	}
	known, err := knownAutomationIDs(ctx, client, draft.userID)
	if err != nil {
		return err
	}
	clashes, err := commandClashIssues(ctx, client, draft)
	if err != nil {
		return err
	}
	issues = append(issues, dups...)
	issues = append(issues, clashes...)
	issues = append(issues, unknownRunTargets(draft.def, known)...)
	if len(issues) == 0 {
		return nil
	}
	return &AutomationRefusal{Code: refusalCode(issues), Issues: sortIssues(issues)}
}

// clashClaim is one draft's names, checked against every sibling recipe in turn.
type clashClaim struct {
	lefts, triggers, owned []string
}

func newClashClaim(def automation.Definition) clashClaim {
	return clashClaim{
		lefts:    normalizedNames(automation.LeftCommandNames(def)),
		triggers: automation.TriggerCommandNames(def),
		owned:    automation.OwnedCooldownNames(def),
	}
}

func (c clashClaim) empty() bool { return len(c.lefts)+len(c.triggers)+len(c.owned) == 0 }

// issuesAgainst keeps a time left command, and a named cooldown, from sharing a name with row's; two recipes may still share a trigger, and a cooldown_add step naming a timer with no owner still saves.
func (c clashClaim) issuesAgainst(row *ent.Automation) []automation.Issue {
	def, parseIssues := automation.Parse([]byte(row.Definition))
	if len(parseIssues) > 0 {
		return nil
	}
	otherLefts := normalizedNames(automation.LeftCommandNames(def))
	var issues []automation.Issue
	if c.leftClashes(otherLefts, def) {
		issues = append(issues, automation.Issue{Path: "left_command", Code: automation.CodeCommandExists})
	}
	if sharesName(c.triggers, otherLefts) {
		issues = append(issues, automation.Issue{Path: "trigger.params.name", Code: automation.CodeCommandExists})
	}
	if sharesName(c.owned, automation.OwnedCooldownNames(def)) {
		issues = append(issues, automation.Issue{Path: "cooldown_name", Code: codeTimerExists})
	}
	return issues
}

func (c clashClaim) leftClashes(otherLefts []string, def automation.Definition) bool {
	return sharesName(c.lefts, otherLefts) || sharesName(c.lefts, automation.TriggerCommandNames(def))
}

func commandClashIssues(ctx context.Context, client *ent.AutomationClient, draft automationDraft) ([]automation.Issue, error) {
	claim := newClashClaim(draft.def)
	if claim.empty() {
		return nil, nil
	}
	rows, err := client.Query().Where(entautomation.UserID(draft.userID), entautomation.IDNEQ(draft.in.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	var issues []automation.Issue
	for _, row := range rows {
		issues = append(issues, claim.issuesAgainst(row)...)
	}
	return slices.Compact(issues), nil
}

func normalizedNames(names []string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = automation.NormalizeCommandName(name)
	}
	return out
}

func sharesName(names, others []string) bool {
	return slices.ContainsFunc(names, func(name string) bool { return slices.Contains(others, name) })
}

// conflictCodes names every issue code a dashboard treats as a naming conflict rather than a plain validation error.
var conflictCodes = map[string]bool{"duplicate": true, automation.CodeCommandExists: true, codeTimerExists: true}

func refusalCode(issues []automation.Issue) rpc.Code {
	for _, issue := range issues {
		if conflictCodes[issue.Code] {
			return rpc.CodeConflict
		}
	}
	return rpc.CodeInvalid
}

// Two concurrent imports would each count a stale total and both insert past the cap, so the count holds the row locks.
func channelAtCap(ctx context.Context, client *ent.AutomationClient, userID uint64, capCheck channelCapCheck) (bool, error) {
	query := client.Query().Where(entautomation.UserID(userID))
	if capCheck == capLocked {
		ids, err := query.ForUpdate().IDs(ctx)
		return len(ids) >= automation.MaxAutomationsPerChannel, err
	}
	n, err := query.Count(ctx)
	return n >= automation.MaxAutomationsPerChannel, err
}

func duplicateIssues(ctx context.Context, client *ent.AutomationClient, draft automationDraft) ([]automation.Issue, error) {
	taken, err := client.Query().
		Where(entautomation.UserID(draft.userID), entautomation.IDNEQ(draft.in.ID), entautomation.NameKey(strings.ToLower(draft.in.Name))).
		Exist(ctx)
	if err != nil || !taken {
		return nil, err
	}
	return []automation.Issue{{Path: "name", Code: "duplicate"}}, nil
}

func knownAutomationIDs(ctx context.Context, client *ent.AutomationClient, userID uint64) (map[string]bool, error) {
	ids, err := client.Query().Where(entautomation.UserID(userID)).IDs(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	return known, nil
}

func unknownRunTargets(def automation.Definition, known map[string]bool) []automation.Issue {
	var issues []automation.Issue
	walkActions(def.Actions, "actions", func(path string, action automation.Action) {
		if action.Type != actionRun {
			return
		}
		var params struct {
			AutomationID string `json:"automation_id"`
		}
		_ = codec.Unmarshal(action.Params, &params)
		if !known[params.AutomationID] {
			issues = append(issues, automation.Issue{Path: path + ".params.automation_id", Code: "unknown_automation"})
		}
	})
	return issues
}

type nestedActions struct {
	Then     []automation.Action `json:"then"`
	Else     []automation.Action `json:"else"`
	Branches []struct {
		Actions []automation.Action `json:"actions"`
	} `json:"branches"`
}

func walkActions(actions []automation.Action, prefix string, visit func(path string, action automation.Action)) {
	for i, action := range actions {
		path := fmt.Sprintf("%s[%d]", prefix, i)
		visit(path, action)
		var nested nestedActions
		if codec.Unmarshal(action.Params, &nested) != nil {
			continue
		}
		walkActions(nested.Then, path+".params.then", visit)
		walkActions(nested.Else, path+".params.else", visit)
		for j, branch := range nested.Branches {
			walkActions(branch.Actions, fmt.Sprintf("%s.params.branches[%d].actions", path, j), visit)
		}
	}
}
