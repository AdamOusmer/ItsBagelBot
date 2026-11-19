// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import "strings"

type PollAction struct {
	Title         string   `json:"title"`
	Choices       []string `json:"choices"`
	Seconds       int      `json:"seconds"`
	PointsPerVote int      `json:"points_per_vote,omitempty"`
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
	Status  string `json:"status"`
	Winner  int    `json:"winner"`
	Outcome string `json:"outcome,omitempty"`
}

type LoginToggleAction struct {
	TargetAction
	On bool `json:"on"`
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

type chatModeValues []span

var chatModes = map[string]chatModeValues{
	"emote_only":  nil,
	"followers":   {between(0, 129600)},
	"slow":        {between(0, 0), between(3, 120)},
	"subscribers": nil,
	"unique":      nil,
	"chat_delay":  {between(2, 2), between(4, 4), between(6, 6)},
}

func (ranges chatModeValues) accepts(value quantity) bool {
	for _, limits := range ranges {
		if value >= limits.lo && value <= limits.hi {
			return true
		}
	}
	return len(ranges) == 0
}

func checkPoll(c chk, p PollAction) {
	c.text("title", p.Title, between(1, 60))
	inRange(c, "seconds", p.Seconds, between(15, 1800))
	c.stringList("choices", p.Choices, 5, func(f field, s string) { c.text(f, s, between(1, 25)) })
	c.count("choices", quantity(len(p.Choices)), between(2, 5))
	inRange(c, "points_per_vote", p.PointsPerVote, between(0, 1000000))
}

func checkPollEnd(c chk, p PollEndAction) { c.oneOf("status", p.Status, enum{"terminate", "archive"}) }

func checkPrediction(c chk, p PredictionAction) {
	c.text("title", p.Title, between(1, 45))
	inRange(c, "seconds", p.Seconds, between(1, 1800))
	c.stringList("outcomes", p.Outcomes, 10, func(f field, s string) { c.text(f, s, between(1, 25)) })
	c.count("outcomes", quantity(len(p.Outcomes)), between(2, 10))
}

func checkPredictionEnd(c chk, p PredictionEndAction) {
	c.oneOf("status", p.Status, enum{"lock", "cancel", "resolve"})
	c.text("outcome", p.Outcome, upTo(100))
	if p.Status != "resolve" || strings.TrimSpace(p.Outcome) != "" {
		return
	}
	if p.Winner == 0 {
		c.add("winner", CodeRequired)
		return
	}
	inRange(c, "winner", p.Winner, between(1, 10))
}

func checkTargetToggle(c chk, p LoginToggleAction) { c.target(p.TargetAction, loginRequired) }

func checkUnbanResolve(c chk, p UnbanResolveAction) {
	c.oneOf("status", p.Status, enum{"approve", "deny"})
	c.text("resolution", p.Resolution, between(0, 500))
}

func checkBrandedContent(chk, BrandedContentAction) {}

func checkChatMode(c chk, p ChatModeAction) {
	values, known := chatModes[p.Mode]
	switch {
	case p.Mode == "":
		c.add("mode", CodeRequired)
	case !known:
		c.add("mode", CodeInvalidEnum)
	case p.On && !values.accepts(quantity(p.Value)):
		c.add("value", CodeOutOfRange)
	}
}

func checkWhisper(c chk, p WhisperAction) {
	if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(p.Login), "@")) == "" {
		p.Login = ""
	}
	c.login(p.Login, loginRequired)
	c.template("text", p.Text)
}
