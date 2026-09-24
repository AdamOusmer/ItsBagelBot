// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/tmpl"
)

type Pure struct {
	Now    func() time.Time
	Locale string
}

var pureUtils = map[string]func(Pure, string) string{
	"math":        func(_ Pure, payload string) string { return evalMath(payload) },
	"queryescape": func(_ Pure, payload string) string { return url.QueryEscape(payload) },
	"pathescape":  func(_ Pure, payload string) string { return url.PathEscape(payload) },
	"repeat":      func(_ Pure, payload string) string { return repeatPhrase(payload) },
	"countdown":   Pure.countdownUtil,
	"countup":     Pure.countupUtil,
}

func (p Pure) countdownUtil(payload string) string { return p.countdown(payload) }
func (p Pure) countupUtil(payload string) string   { return p.countup(payload) }

func (Pure) Owns(v Var) bool {
	name := v.Name
	if name == "random" || name == "choice" {
		return true
	}
	_, ok := pureUtils[name]
	return ok
}

func (p Pure) Plan(context.Context, []Var) (Values, error) {
	return pureValues{p: p}, nil
}

type pureValues struct{ p Pure }

func (v pureValues) Get(tok Var) (string, bool) {
	util, ok := pureUtils[tok.Name]
	if !ok {
		return tmpl.Dynamic(tok)
	}
	if !tok.HasPayload {
		return "", false
	}
	return util(v.p, tok.Payload), true
}

const (
	MaxRepeatCount = 20
	MaxRepeatBytes = 480
)

func repeatPhrase(payload string) string {
	countText, phrase, ok := strings.Cut(payload, ":")
	if !ok || phrase == "" {
		return ""
	}
	n, ok := repeatCount(countText)
	if !ok || n*len(phrase)+n-1 > MaxRepeatBytes {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = phrase
	}
	return strings.Join(parts, " ")
}

func repeatCount(text string) (int, bool) {
	if text == "" || strings.TrimLeft(text, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	if err != nil || outsideRepeatRange(n) {
		return 0, false
	}
	return n, true
}

func outsideRepeatRange(n int) bool {
	return n < 1 || n > MaxRepeatCount
}
