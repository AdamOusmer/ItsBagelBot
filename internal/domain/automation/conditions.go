// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"regexp"
	"strconv"
)

var (
	cmpOps        = enum{"eq", "ne", "gt", "gte", "lt", "lte"}
	permLevels    = enum{"everyone", "sub", "vip", "mod", "lead_mod", "broadcaster"}
	liveStates    = enum{"live", "offline"}
	loginPattern  = regexp.MustCompile(`^[a-z0-9_]{1,25}$`)
	userIDPattern = regexp.MustCompile(`^[0-9]{1,19}$`)
)

type PermCondition struct {
	Level string `json:"level"`
}

type UsersCondition struct {
	Allow    []string `json:"allow"`
	Deny     []string `json:"deny"`
	AllowIDs []string `json:"allow_ids"`
	DenyIDs  []string `json:"deny_ids"`

	AllowUserIDs []uint64 `json:"-"`
	DenyUserIDs  []uint64 `json:"-"`
}

func (c *UsersCondition) prepare() {
	c.AllowUserIDs = parseUserIDs(c.AllowIDs)
	c.DenyUserIDs = parseUserIDs(c.DenyIDs)
}

func parseUserIDs(raw []string) []uint64 {
	ids := make([]uint64, 0, len(raw))
	for _, s := range raw {
		if id, err := strconv.ParseUint(s, 10, 64); err == nil && id != 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

type LiveCondition struct {
	State string `json:"state"`
}

type CooldownCondition struct {
	GlobalS     int    `json:"global_s"`
	UserS       int    `json:"user_s"`
	LeftCommand string `json:"left_command,omitempty"`
	LeftReply   string `json:"left_reply,omitempty"`
	ReadyReply  string `json:"ready_reply,omitempty"`
}

type ChanceCondition struct {
	Percent int `json:"percent"`
}

type CounterCondition struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value int64  `json:"value"`
}

type PointsCondition struct {
	Op    string `json:"op"`
	Value int64  `json:"value"`
}

type ArgRegexCondition struct {
	Pattern  string         `json:"pattern"`
	Compiled *regexp.Regexp `json:"-"`
}

func (c *ArgRegexCondition) prepare() {
	if re, err := regexp.Compile(c.Pattern); err == nil {
		c.Compiled = re
	}
}

type CategoryCondition struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

func checkPerm(c chk, p PermCondition) { c.oneOf("level", p.Level, permLevels) }

func checkLive(c chk, p LiveCondition) { c.oneOf("state", p.State, liveStates) }

func checkChance(c chk, p ChanceCondition) { inRange(c, "percent", p.Percent, between(1, 100)) }

func checkArgRegex(c chk, p ArgRegexCondition) { c.regex("pattern", p.Pattern, MaxPatternLength) }

func checkPointsCond(c chk, p PointsCondition) { c.oneOf("op", p.Op, cmpOps) }

func checkCounterCond(c chk, p CounterCondition) {
	c.counterName("name", p.Name)
	c.oneOf("op", p.Op, cmpOps)
}

func checkCooldown(c chk, p CooldownCondition) {
	inRange(c, "global_s", p.GlobalS, between(0, MaxCooldownSeconds))
	inRange(c, "user_s", p.UserS, between(0, MaxCooldownSeconds))
	c.atLeastOne(p.GlobalS > 0 || p.UserS > 0)
	if p.LeftCommand != "" {
		c.commandName("left_command", p.LeftCommand, "start")
	}
	c.text("left_reply", p.LeftReply, upTo(MaxTemplateLength))
	c.text("ready_reply", p.ReadyReply, upTo(MaxTemplateLength))
}

func checkUsers(c chk, p UsersCondition) {
	c.atLeastOne(len(p.Allow)+len(p.Deny)+len(p.AllowIDs)+len(p.DenyIDs) > 0)
	each := c.eachFormat(loginPattern)
	c.stringList("allow", p.Allow, MaxListEntries, each)
	c.stringList("deny", p.Deny, MaxListEntries, each)
	c.stringList("allow_ids", p.AllowIDs, MaxListEntries, c.eachUserID)
	c.stringList("deny_ids", p.DenyIDs, MaxListEntries, c.eachUserID)
}

func (c chk) eachUserID(f field, s string) {
	c.format(f, s, userIDPattern)
	if id, err := strconv.ParseUint(s, 10, 64); err == nil && id == 0 {
		c.add(f, CodeOutOfRange)
	}
}

func checkCategory(c chk, p CategoryCondition) {
	c.atLeastOne(len(p.Allow)+len(p.Deny) > 0)
	each := c.eachText(nonEmpty(100))
	c.stringList("allow", p.Allow, 50, each)
	c.stringList("deny", p.Deny, 50, each)
}

func (c chk) counterName(f field, name string) {
	if c.text(f, name, nonEmpty(64)) {
		c.chars(f, name, isCounterChar)
	}
}
