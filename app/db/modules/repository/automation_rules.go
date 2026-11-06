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

// commandClashIssues keeps a time left command from sharing a name with another recipe; two recipes may still share a trigger.
func commandClashIssues(ctx context.Context, client *ent.AutomationClient, draft automationDraft) ([]automation.Issue, error) {
	lefts := normalizedNames(automation.LeftCommandNames(draft.def))
	triggers := automation.TriggerCommandNames(draft.def)
	if len(lefts)+len(triggers) == 0 {
		return nil, nil
	}
	rows, err := client.Query().Where(entautomation.UserID(draft.userID), entautomation.IDNEQ(draft.in.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	var issues []automation.Issue
	for _, row := range rows {
		def, parseIssues := automation.Parse([]byte(row.Definition))
		if len(parseIssues) > 0 {
			continue
		}
		otherLefts := normalizedNames(automation.LeftCommandNames(def))
		if sharesName(lefts, otherLefts) || sharesName(lefts, automation.TriggerCommandNames(def)) {
			issues = append(issues, automation.Issue{Path: "left_command", Code: automation.CodeCommandExists})
		}
		if sharesName(triggers, otherLefts) {
			issues = append(issues, automation.Issue{Path: "trigger.params.name", Code: automation.CodeCommandExists})
		}
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

func refusalCode(issues []automation.Issue) rpc.Code {
	for _, issue := range issues {
		if issue.Code == "duplicate" || issue.Code == automation.CodeCommandExists {
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
