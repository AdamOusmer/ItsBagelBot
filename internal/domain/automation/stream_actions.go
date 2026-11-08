// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

type PollAction struct {
	Title   string   `json:"title"`
	Choices []string `json:"choices"`
	Seconds int      `json:"seconds"`
}

type PollEndAction struct {
	Status string `json:"status"`
}

type PredictionAction struct {
	Title    string   `json:"title"`
	Outcomes []string `json:"outcomes"`
	Seconds  int      `json:"seconds"`
}

type PredictionEndAction struct {
	Status string `json:"status"`
	Winner int    `json:"winner"`
}

type LoginToggleAction struct {
	Login string `json:"login"`
	On    bool   `json:"on"`
}

type UnbanResolveAction struct {
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
}

type BrandedContentAction struct {
	On bool `json:"on"`
}

type ChatModeAction struct {
	Mode  string `json:"mode"`
	On    bool   `json:"on"`
	Value int    `json:"value"`
}

type WhisperAction struct {
	Login string `json:"login"`
	Text  string `json:"text"`
}

var chatModes = enum{"emote_only", "followers", "slow", "subscribers", "unique"}

func checkPoll(c chk, p PollAction) {
	c.text("title", p.Title, between(1, 60))
	inRange(c, "seconds", p.Seconds, between(15, 1800))
	c.stringList("choices", p.Choices, 5, func(f field, s string) { c.text(f, s, between(1, 25)) })
	c.count("choices", quantity(len(p.Choices)), between(2, 5))
}

func checkPollEnd(c chk, p PollEndAction) { c.oneOf("status", p.Status, enum{"terminate", "archive"}) }

func checkPrediction(c chk, p PredictionAction) {
	c.text("title", p.Title, between(1, 45))
	inRange(c, "seconds", p.Seconds, between(30, 1800))
	c.stringList("outcomes", p.Outcomes, 10, func(f field, s string) { c.text(f, s, between(1, 25)) })
	c.count("outcomes", quantity(len(p.Outcomes)), between(2, 10))
}

func checkPredictionEnd(c chk, p PredictionEndAction) {
	c.oneOf("status", p.Status, enum{"lock", "cancel", "resolve"})
	if p.Status != "resolve" {
		return
	}
	if p.Winner == 0 {
		c.add("winner", CodeRequired)
		return
	}
	inRange(c, "winner", p.Winner, between(1, 10))
}

func checkLoginToggle(c chk, p LoginToggleAction) { c.text("login", p.Login, loginRequired) }

func checkUnbanResolve(c chk, p UnbanResolveAction) {
	c.oneOf("status", p.Status, enum{"approve", "deny"})
	c.text("resolution", p.Resolution, between(0, 500))
}

func checkBrandedContent(chk, BrandedContentAction) {}

func checkChatMode(c chk, p ChatModeAction) {
	c.oneOf("mode", p.Mode, chatModes)
	inRange(c, "value", p.Value, between(0, 86400))
}

func checkWhisper(c chk, p WhisperAction) {
	c.text("login", p.Login, loginRequired)
	c.template("text", p.Text)
}
