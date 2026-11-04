// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
)

type EventField struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Numeric bool   `json:"numeric"`
}

type EventKind struct {
	Kind   string       `json:"kind"`
	Fields []EventField `json:"fields"`
}

type fieldName string

type payloadPath string

type eventName string

func text(name fieldName, path payloadPath) EventField {
	return EventField{Name: string(name), Path: string(path)}
}

func num(name fieldName, path payloadPath) EventField {
	return EventField{Name: string(name), Path: string(path), Numeric: true}
}

func viewer(extra ...EventField) []EventField {
	base := []EventField{text("user", "user_name"), text("login", "user_login"), text("user_id", "user_id")}
	return append(base, extra...)
}

func kind(name eventName, fields ...EventField) EventKind {
	return EventKind{Kind: string(name), Fields: append([]EventField{}, fields...)}
}

var eventKinds = []EventKind{
	kind("channel.follow", viewer()...),
	kind("channel.subscribe", viewer(text("tier", "tier"), text("is_gift", "is_gift"))...),
	kind("channel.subscription.message", viewer(
		text("tier", "tier"), num("months", "cumulative_months"), num("streak", "streak_months"),
		text("message", "message.text"))...),
	kind("channel.subscription.gift", viewer(
		text("tier", "tier"), num("amount", "total"), num("total", "cumulative_total"),
		text("anonymous", "is_anonymous"))...),
	kind("channel.cheer", viewer(
		num("amount", "bits"), text("message", "message"), text("anonymous", "is_anonymous"))...),
	kind("channel.raid",
		text("user", "from_broadcaster_user_name"), text("login", "from_broadcaster_user_login"),
		text("user_id", "from_broadcaster_user_id"), num("viewers", "viewers")),
	kind("channel.ad_break.begin", num("seconds", "duration_seconds"), text("automatic", "is_automatic")),
	kind("channel.channel_points_custom_reward_redemption.add", viewer(
		text("reward", "reward.title"), text("reward_id", "reward.id"), num("cost", "reward.cost"),
		text("input", "user_input"))...),
	kind("stream.online", text("type", "type")),
	kind("stream.offline"),
}

var eventIndex map[string]EventKind

func init() {
	eventIndex = make(map[string]EventKind, len(eventKinds))
	for _, k := range eventKinds {
		eventIndex[k.Kind] = k
	}
}

func EventKinds() []EventKind {
	out := make([]EventKind, len(eventKinds))
	for i, k := range eventKinds {
		out[i] = kind(eventName(k.Kind), k.Fields...)
	}
	slices.SortFunc(out, func(a, b EventKind) int { return strings.Compare(a.Kind, b.Kind) })
	return out
}

func LookupEventKind(name string) (EventKind, bool) {
	k, ok := eventIndex[name]
	return k, ok
}

func (k EventKind) field(name string) (EventField, bool) {
	i := slices.IndexFunc(k.Fields, func(f EventField) bool { return f.Name == name })
	if i < 0 {
		return EventField{}, false
	}
	return k.Fields[i], true
}

func (k EventKind) Extract(event []byte) map[string]string {
	out := make(map[string]string, len(k.Fields))
	dec := json.NewDecoder(bytes.NewReader(event))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return out
	}
	for _, f := range k.Fields {
		if s, ok := render(walk(root, f.Path)); ok {
			out[f.Name] = s
		}
	}
	return out
}

func walk(node any, path string) any {
	for _, seg := range strings.Split(path, ".") {
		obj, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		node = obj[seg]
	}
	return node
}

func render(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case json.Number:
		return renderNumber(t), true
	}
	return "", false
}

func renderNumber(n json.Number) string {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return strconv.FormatInt(i, 10)
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || !wholeNumber(f) {
		return n.String()
	}
	return strconv.FormatInt(int64(f), 10)
}

func wholeNumber(f float64) bool {
	if f != math.Trunc(f) {
		return false
	}
	return math.Abs(f) < 1e18
}
