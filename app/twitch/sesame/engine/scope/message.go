// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"net/url"
	"strings"
)

const MaxPositional = 30

// Callers must sanitize values (engine.sanitizeVar): a leading slash would run a moderation verb.
type Message struct {
	User    string
	Sender  string
	Words   []string
	Touser  string
	Channel string
	UserID  string
	Login   string
	Command string
}

var messageFields = map[string]func(Message) string{
	"user":        func(m Message) string { return m.User },
	"sender":      func(m Message) string { return m.Sender },
	"args":        func(m Message) string { return m.rest(1) },
	"touser":      func(m Message) string { return m.Touser },
	"target":      func(m Message) string { return m.Touser },
	"channel":     func(m Message) string { return m.Channel },
	"user.id":     func(m Message) string { return m.UserID },
	"user.login":  func(m Message) string { return m.Login },
	"command":     func(m Message) string { return m.Command },
	"querystring": func(m Message) string { return url.QueryEscape(m.rest(1)) },
}

var messageAliases = map[string]string{
	"userid": "user.id",
}

func canonicalName(name string) string {
	if canon, ok := messageAliases[name]; ok {
		return canon
	}
	return name
}

func (Message) Owns(v Var) bool {
	name := v.Name
	if _, ok := positionalIndex(name); ok {
		return true
	}
	if name == "" {
		return true
	}
	_, ok := messageFields[canonicalName(name)]
	return ok
}

func (m Message) Plan(context.Context, []Var) (Values, error) {
	return m, nil
}

func (m Message) Get(v Var) (string, bool) {
	if n, ok := positionalIndex(v.Name); ok {
		return m.positional(n, v)
	}
	if v.Name == "" {
		return m.leadingSlice(v)
	}
	if v.HasPayload {
		return "", false
	}
	field, ok := messageFields[canonicalName(v.Name)]
	if !ok {
		return "", false
	}
	return field(m), true
}

func (m Message) positional(n int, v Var) (string, bool) {
	switch {
	case !v.HasPayload:
		return m.word(n), true
	case v.Payload == "":
		return m.rest(n), true
	default:
		return m.boundedSlice(n, v.Payload)
	}
}

func (m Message) leadingSlice(v Var) (string, bool) {
	if !v.HasPayload {
		return "", false
	}
	end, ok := positionalIndex(v.Payload)
	if !ok {
		return "", false
	}
	return m.slice(1, end), true
}

func (m Message) boundedSlice(n int, payload string) (string, bool) {
	end, ok := positionalIndex(payload)
	if !ok || end < n {
		return "", false
	}
	return m.slice(n, end), true
}

func (m Message) word(n int) string {
	if n > len(m.Words) {
		return ""
	}
	return m.Words[n-1]
}

func (m Message) rest(n int) string {
	return m.slice(n, len(m.Words))
}

func (m Message) slice(n, end int) string {
	if n > len(m.Words) {
		return ""
	}
	if end > len(m.Words) {
		end = len(m.Words)
	}
	return strings.Join(m.Words[n-1:end], " ")
}

func positionalIndex(name string) (int, bool) {
	if len(name) > 2 || strings.HasPrefix(name, "0") {
		return 0, false
	}
	n := 0
	for i := 0; i < len(name); i++ {
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
		n = n*10 + int(name[i]-'0')
	}
	if n < 1 || n > MaxPositional {
		return 0, false
	}
	return n, true
}
