// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"regexp"
	"strings"
)

type TargetAction struct {
	Login  string `json:"login"`
	Who    string `json:"who,omitempty"`
	UserID string `json:"user_id,omitempty"`
}

var targetUserIDPattern = regexp.MustCompile(`^[0-9]+$`)

func (p *TargetAction) setDefaults() { p.Who = "login" }

func (p TargetAction) targetsViewer() bool {
	switch p.Who {
	case "viewer":
		return true
	case "login":
		return strings.TrimSpace(p.Login) == ""
	}
	return false
}

func (c chk) target(p TargetAction, legacyLogin span) {
	switch p.Who {
	case "login":
		c.text("login", p.Login, legacyLogin)
	case "viewer", "mention", "user":
		c.text("login", p.Login, loginOptional)
	default:
		c.add("who", CodeInvalidEnum)
		return
	}
	if p.Who == "user" || p.UserID != "" {
		c.format("user_id", p.UserID, targetUserIDPattern)
	}
}
