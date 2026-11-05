// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/validate"
)

type LoopAction struct {
	Times   string   `json:"times"`
	Actions []Action `json:"actions"`
}

func checkLoop(c chk, p LoopAction) {
	c.template("times", p.Times)
	if isStaticText(p.Times) && p.Times != "" {
		if _, ok := ParseLoopTimes(p.Times); !ok {
			c.add("times", CodeInvalidFormat)
		}
	}
	c.sub("actions").nest().actions(p.Actions)
}

func loopBranches(p LoopAction) nesting {
	return nesting{lists: [][]Action{p.Actions}, labels: []string{"body"}}
}

func isStaticText(s string) bool { return !strings.Contains(s, "{") }

// ParseLoopTimes reads a rendered times value, clamped to 0..MaxLoopTimes.
func ParseLoopTimes(rendered string) (int, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(rendered), 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return int(min(max(value, 0), MaxLoopTimes)), true
}

type HTTPGetAction struct {
	URL       string   `json:"url"`
	JSONPath  []string `json:"json_path,omitempty"`
	SaveAs    string   `json:"save_as,omitempty"`
	OnOK      []Action `json:"on_ok,omitempty"`
	OnFailure []Action `json:"on_failure,omitempty"`
}

func checkHTTPGet(c chk, p HTTPGetAction) {
	if c.text("url", p.URL, nonEmpty(validate.MaxFetchURLLength)) && !plausibleFetchTemplate(p.URL) {
		c.add("url", CodeInvalidFormat)
	}
	if validate.FetchPath(p.JSONPath) != nil {
		c.add("json_path", CodeInvalidFormat)
	}
	c.optionalVariableName("save_as", p.SaveAs)
	c.fetchPaths(p.OnOK, p.OnFailure)
}

// A template's final URL is only known at run time; the engine and gossip check it again then.
func plausibleFetchTemplate(raw string) bool {
	if isStaticText(raw) {
		return validate.FetchURL(raw) == nil
	}
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "{")
}

func (c chk) fetchPaths(onOK, onFailure []Action) {
	c.sub("on_ok").nest().actions(onOK)
	c.sub("on_failure").nest().actions(onFailure)
}

func httpGetBranches(p HTTPGetAction) nesting { return fetchNesting(p.OnOK, p.OnFailure) }

func httpPostBranches(p HTTPPostAction) nesting { return fetchNesting(p.OnOK, p.OnFailure) }

func fetchNesting(onOK, onFailure []Action) nesting {
	return nesting{lists: [][]Action{onOK, onFailure}, labels: []string{"on_ok", "on_failure"}}
}
