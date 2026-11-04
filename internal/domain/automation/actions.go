// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/codec"
)

var (
	announceColors   = []string{"", "primary", "blue", "green", "orange", "purple"}
	commercialLength = []int{30, 60, 90, 120, 150, 180}

	lightsModes        = enum{LightsModeColor, LightsModeScene}
	lightsColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

type TextAction struct {
	Text string `json:"text"`
}

type AnnounceAction struct {
	Text  string `json:"text"`
	Color string `json:"color"`
}

type TimeoutAction struct {
	Login   string `json:"login"`
	Seconds int    `json:"seconds"`
	Reason  string `json:"reason"`
}

type BanAction struct {
	Login  string `json:"login"`
	Reason string `json:"reason"`
}

type LoginAction struct {
	Login string `json:"login"`
}

type WarnAction struct {
	Login  string `json:"login"`
	Reason string `json:"reason"`
}

type ShieldAction struct {
	On bool `json:"on"`
}

type MarkerAction struct {
	Description string `json:"description"`
}

type SetTitleAction struct {
	Title string `json:"title"`
}

type SetGameAction struct {
	Game string `json:"game"`
}

type SetTagsAction struct {
	Tags string `json:"tags"`
}

type CommercialAction struct {
	Seconds int `json:"seconds"`
}

type PointsAddAction struct {
	Login  string `json:"login"`
	Amount int64  `json:"amount"`
}

type PointsSetAction struct {
	Login  string `json:"login"`
	Amount int64  `json:"amount"`
}

type CounterAddAction struct {
	Name   string `json:"name"`
	Amount int64  `json:"amount"`
}

type CounterSetAction struct {
	Name  string `json:"name"`
	Value int64  `json:"value"`
}

type RedemptionAction struct {
	Status string `json:"status"`
}

type DelayAction struct {
	Ms int `json:"ms"`
}

type Branch struct {
	Weight  int      `json:"weight"`
	Actions []Action `json:"actions"`
}

type RandomPickAction struct {
	Branches []Branch `json:"branches"`
}

type ParallelAction struct {
	Branches []Branch `json:"branches"`
}

type RunAction struct {
	AutomationID string `json:"automation_id"`
}

type IfAction struct {
	Condition Condition `json:"condition"`
	Then      []Action  `json:"then"`
	Else      []Action  `json:"else"`
}

type HTTPPostAction struct {
	URL  string `json:"url"`
	Body string `json:"body"`
}

const (
	LightsModeColor = "color"
	LightsModeScene = "scene"
)

type LightsAction struct {
	Device        string `json:"device"`
	SKU           string `json:"sku"`
	DeviceName    string `json:"device_name"`
	Mode          string `json:"mode"`
	Color         string `json:"color"`
	SceneID       string `json:"scene_id"`
	SceneName     string `json:"scene_name"`
	SceneType     string `json:"scene_type"`
	SceneInstance string `json:"scene_instance"`
	SceneValue    string `json:"scene_value"`
	RGB           int    `json:"-"`
}

func (a *LightsAction) setDefaults() { a.Mode = LightsModeColor }

func (a *LightsAction) prepare() {
	if v, err := strconv.ParseUint(strings.TrimPrefix(a.Color, "#"), 16, 24); err == nil {
		a.RGB = int(v)
	}
}

var (
	loginRequired = nonEmpty(MaxLoginTemplate)
	loginOptional = upTo(MaxLoginTemplate)
	reasonOptions = upTo(MaxTemplateLength)
	reasonNeeded  = nonEmpty(MaxTemplateLength)
)

func checkText(c chk, p TextAction) { c.template("text", p.Text) }

func checkAnnounce(c chk, p AnnounceAction) {
	c.template("text", p.Text)
	if !slices.Contains(announceColors, p.Color) {
		c.add("color", CodeInvalidEnum)
	}
}

func checkTimeout(c chk, p TimeoutAction) {
	c.text("login", p.Login, loginRequired)
	inRange(c, "seconds", p.Seconds, between(1, 1209600))
	c.text("reason", p.Reason, reasonOptions)
}

func checkBan(c chk, p BanAction) {
	c.text("login", p.Login, loginRequired)
	c.text("reason", p.Reason, reasonOptions)
}

func checkWarn(c chk, p WarnAction) {
	c.text("login", p.Login, loginRequired)
	c.text("reason", p.Reason, reasonNeeded)
}

func checkLogin(c chk, p LoginAction) { c.text("login", p.Login, loginRequired) }

func checkShield(chk, ShieldAction) {}

func checkMarker(c chk, p MarkerAction) { c.text("description", p.Description, upTo(140)) }

func checkSetTitle(c chk, p SetTitleAction) { c.text("title", p.Title, nonEmpty(140)) }

func checkSetGame(c chk, p SetGameAction) { c.text("game", p.Game, nonEmpty(100)) }

func checkSetTags(c chk, p SetTagsAction) { c.template("tags", p.Tags) }

func checkCommercial(c chk, p CommercialAction) {
	if !slices.Contains(commercialLength, p.Seconds) {
		c.add("seconds", CodeInvalidEnum)
	}
}

func checkPointsAdd(c chk, p PointsAddAction) {
	c.text("login", p.Login, loginOptional)
	inRange(c, "amount", p.Amount, between(-1000000, 1000000))
	nonZero(c, "amount", p.Amount)
}

func checkPointsSet(c chk, p PointsSetAction) {
	c.text("login", p.Login, loginOptional)
	inRange(c, "amount", p.Amount, between(0, 1000000000))
}

func checkCounterAdd(c chk, p CounterAddAction) {
	c.counterName("name", p.Name)
	inRange(c, "amount", p.Amount, between(-1000000, 1000000))
	nonZero(c, "amount", p.Amount)
}

func checkCounterSet(c chk, p CounterSetAction) { c.counterName("name", p.Name) }

func checkRedemption(c chk, p RedemptionAction) {
	c.oneOf("status", p.Status, enum{"fulfill", "cancel"})
}

func checkDelay(c chk, p DelayAction) { inRange(c, "ms", p.Ms, between(1, MaxDelayMS)) }

func checkRun(c chk, p RunAction) { c.text("automation_id", p.AutomationID, nonEmpty(64)) }

func checkHTTPPost(c chk, p HTTPPostAction) {
	if c.text("url", p.URL, nonEmpty(validate.MaxFetchURLLength)) && validate.FetchURL(p.URL) != nil {
		c.add("url", CodeInvalidFormat)
	}
	c.text("body", p.Body, upTo(2000))
}

func checkLights(c chk, p LightsAction) {
	c.text("device", p.Device, nonEmpty(64))
	c.text("sku", p.SKU, nonEmpty(32))
	c.text("device_name", p.DeviceName, upTo(100))
	c.oneOf("mode", p.Mode, lightsModes)
	if p.Mode == LightsModeScene {
		checkLightsScene(c, p)
		return
	}
	c.format("color", p.Color, lightsColorPattern)
}

func checkLightsScene(c chk, p LightsAction) {
	c.text("scene_id", p.SceneID, upTo(100))
	c.text("scene_name", p.SceneName, upTo(100))
	c.text("scene_type", p.SceneType, nonEmpty(64))
	c.text("scene_instance", p.SceneInstance, nonEmpty(64))
	if c.text("scene_value", p.SceneValue, nonEmpty(200)) && !codec.Valid([]byte(p.SceneValue)) {
		c.add("scene_value", CodeInvalidFormat)
	}
}

func checkRandomPick(c chk, p RandomPickAction) {
	if !c.count("branches", quantity(len(p.Branches)), between(2, 5)) {
		return
	}
	for i, b := range p.Branches {
		at := c.sub("branches").elem(index(i))
		inRange(at, "weight", b.Weight, between(1, 100))
		at.sub("actions").nest().actions(b.Actions)
	}
}

func randomPickBranches(p RandomPickAction) nesting {
	n := nesting{lists: make([][]Action, len(p.Branches)), labels: make([]string, len(p.Branches))}
	for i, b := range p.Branches {
		n.lists[i], n.labels[i] = b.Actions, "b"+strconv.Itoa(i)
	}
	return n
}

func checkParallel(c chk, p ParallelAction) {
	if !c.count("branches", quantity(len(p.Branches)), between(2, 10)) {
		return
	}
	for i, b := range p.Branches {
		c.sub("branches").elem(index(i)).sub("actions").nest().actions(b.Actions)
	}
}

func parallelBranches(p ParallelAction) nesting {
	return randomPickBranches(RandomPickAction(p))
}

func checkIf(c chk, p IfAction) {
	c.sub("condition").typed(conditionRegistry, typedParams(p.Condition))
	c.sub("then").nest().actions(p.Then)
	c.sub("else").nest().actions(p.Else)
}

func ifBranches(p IfAction) nesting {
	return nesting{lists: [][]Action{p.Then, p.Else}, labels: []string{"then", "else"}, condition: &p.Condition}
}
