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
	announceColors     = []string{"", "primary", "blue", "green", "orange", "purple"}
	commercialLength   = []int{30, 60, 90, 120, 150, 180}
	actionLoginPattern = regexp.MustCompile(`^[a-zA-Z0-9_]{4,25}$`)

	lightsModes        = enum{LightsModeColor, LightsModeScene, LightsModeWhite, LightsModeOn, LightsModeOff}
	lightsColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	lightRefPattern    = regexp.MustCompile(`^[^/]{1,64}/[^/]{1,32}$`)
)

type TextAction struct {
	Text string `json:"text"`
}

type ReplyAction struct {
	Text   string `json:"text"`
	Thread bool   `json:"thread,omitempty"`
}

type AnnounceAction struct {
	Text  string `json:"text"`
	Color string `json:"color"`
}

type TimeoutAction struct {
	TargetAction
	Seconds int    `json:"seconds"`
	Reason  string `json:"reason"`
}

type BanAction struct {
	TargetAction
	Reason string `json:"reason"`
}

type UnbanAction struct {
	TargetAction
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

const MaxClipTitleRunes = 100

type ClipAction struct {
	Title   string  `json:"title"`
	Seconds float64 `json:"seconds"`
	Reply   string  `json:"reply"`
	Quiet   bool    `json:"quiet,omitempty"`
}

type MarkerAction struct {
	Description string `json:"description"`
	Quiet       bool   `json:"quiet,omitempty"`
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
	Seconds int  `json:"seconds"`
	Quiet   bool `json:"quiet,omitempty"`
}

type PointsAddAction struct {
	TargetAction
	Amount int64 `json:"amount"`
}

type PointsSetAction PointsAddAction

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
	URL       string   `json:"url"`
	Body      string   `json:"body"`
	SaveAs    string   `json:"save_as,omitempty"`
	OnOK      []Action `json:"on_ok,omitempty"`
	OnFailure []Action `json:"on_failure,omitempty"`
}

const (
	LightsModeColor = "color"
	LightsModeScene = "scene"
	LightsModeWhite = "white"
	LightsModeOn    = "on"
	LightsModeOff   = "off"
	maxSafeSceneID  = 1<<53 - 1
)

const LightsRestoreType = "lights_restore"

type LightsRestoreAction struct {
	Device      string   `json:"device"`
	SKU         string   `json:"sku"`
	ExtraLights []string `json:"extra_lights,omitempty"`
}

type LightsAction struct {
	Device        string   `json:"device"`
	SKU           string   `json:"sku"`
	DeviceName    string   `json:"device_name"`
	ExtraLights   []string `json:"extra_lights,omitempty"`
	Mode          string   `json:"mode"`
	Color         string   `json:"color"`
	SceneID       string   `json:"scene_id"`
	SceneName     string   `json:"scene_name"`
	SceneType     string   `json:"scene_type"`
	SceneInstance string   `json:"scene_instance"`
	SceneValue    string   `json:"scene_value"`
	AnyTime       bool     `json:"any_time,omitempty"`
	Kelvin        int      `json:"kelvin,omitempty"`
	Brightness    int      `json:"brightness,omitempty"`
	HoldS         int      `json:"hold_s,omitempty"`
	RGB           int      `json:"-"`
}

func (a *LightsAction) setDefaults() { a.Mode = LightsModeColor }

func (a *LightsAction) prepare() {
	if v, err := strconv.ParseUint(strings.TrimPrefix(a.Color, "#"), 16, 24); err == nil {
		a.RGB = int(v)
	}
}

func lightsHoldBranch(p LightsAction) nesting {
	if p.HoldS <= 0 || p.HoldS > MaxLightsHoldS {
		return nesting{}
	}
	delay, _ := codec.Marshal(DelayAction{Ms: p.HoldS * 1000})
	restore, _ := codec.Marshal(LightsRestoreAction{Device: p.Device, SKU: p.SKU, ExtraLights: p.ExtraLights})
	return nesting{
		lists:    [][]Action{{{Type: "delay", Params: delay}, {Type: LightsRestoreType, Params: restore}}},
		labels:   []string{"hold"},
		internal: true,
	}
}

var (
	loginRequired = nonEmpty(MaxLoginTemplate)
	loginOptional = upTo(MaxLoginTemplate)
	reasonOptions = upTo(MaxTemplateLength)
	reasonNeeded  = nonEmpty(MaxTemplateLength)
)

func checkText(c chk, p TextAction) { c.template("text", p.Text) }

func checkReply(c chk, p ReplyAction) { checkText(c, TextAction{Text: p.Text}) }

func checkAnnounce(c chk, p AnnounceAction) {
	c.template("text", p.Text)
	if !slices.Contains(announceColors, p.Color) {
		c.add("color", CodeInvalidEnum)
	}
}

func checkTimeout(c chk, p TimeoutAction) {
	c.target(p.TargetAction, loginRequired)
	inRange(c, "seconds", p.Seconds, between(1, 1209600))
	c.text("reason", p.Reason, reasonOptions)
}

func checkBan(c chk, p BanAction) {
	c.target(p.TargetAction, loginRequired)
	c.text("reason", p.Reason, reasonOptions)
}

func checkUnban(c chk, p UnbanAction) { c.target(p.TargetAction, loginRequired) }

func checkWarn(c chk, p WarnAction) {
	c.login(p.Login, loginRequired)
	c.text("reason", p.Reason, reasonNeeded)
}

func checkLogin(c chk, p LoginAction) { c.login(p.Login, loginRequired) }

func (c chk) login(login string, bounds span) {
	if !c.text("login", login, bounds) {
		return
	}
	if login == "" || !isStaticText(login) {
		return
	}
	if !actionLoginPattern.MatchString(strings.TrimPrefix(login, "@")) {
		c.add("login", CodeInvalidFormat)
	}
}

func checkShield(chk, ShieldAction) {}

func checkClip(c chk, p ClipAction) {
	c.text("title", p.Title, upTo(MaxClipTitleRunes))
	if p.Seconds != 0 {
		inRange(c, "seconds", p.Seconds, between(5, 60))
	}
	c.text("reply", p.Reply, upTo(MaxTemplateLength))
}

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
	c.target(p.TargetAction, loginOptional)
	inRange(c, "amount", p.Amount, between(-1000000, 1000000))
	nonZero(c, "amount", p.Amount)
}

func checkPointsSet(c chk, p PointsSetAction) {
	c.target(p.TargetAction, loginOptional)
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
	if c.text("url", p.URL, nonEmpty(validate.MaxFetchURLLength)) && !plausibleFetchTemplate(p.URL) {
		c.add("url", CodeInvalidFormat)
	}
	c.text("body", p.Body, upTo(2000))
	c.optionalVariableName("save_as", p.SaveAs)
	c.fetchPaths(p.OnOK, p.OnFailure)
}

var lightsModeChecks = map[string]func(chk, LightsAction){
	LightsModeColor: func(c chk, p LightsAction) { c.format("color", p.Color, lightsColorPattern) },
	LightsModeScene: checkLightsScene,
	LightsModeWhite: checkLightsWhite,
	LightsModeOff:   func(c chk, p LightsAction) { inRange(c, "brightness", p.Brightness, between(0, 0)) },
}

func checkLights(c chk, p LightsAction) {
	c.text("device", p.Device, nonEmpty(64))
	c.text("sku", p.SKU, nonEmpty(32))
	c.text("device_name", p.DeviceName, upTo(100))
	c.stringList("extra_lights", p.ExtraLights, MaxExtraLights, c.eachFormat(lightRefPattern))
	c.oneOf("mode", p.Mode, lightsModes)
	if p.Mode != LightsModeOff && p.Brightness != 0 {
		inRange(c, "brightness", p.Brightness, between(MinLightsBrightness, MaxLightsBrightness))
	}
	inRange(c, "hold_s", p.HoldS, upTo(MaxLightsHoldS))
	if check := lightsModeChecks[p.Mode]; check != nil {
		check(c, p)
	}
}

func checkLightsScene(c chk, p LightsAction) {
	c.text("scene_id", p.SceneID, upTo(100))
	c.text("scene_name", p.SceneName, upTo(100))
	c.text("scene_type", p.SceneType, nonEmpty(64))
	c.text("scene_instance", p.SceneInstance, nonEmpty(64))
	if c.text("scene_value", p.SceneValue, nonEmpty(200)) && !p.validSceneValue() {
		c.add("scene_value", CodeInvalidFormat)
	}
}

func (p LightsAction) validSceneValue() bool {
	var number *int64
	if err := codec.UnmarshalFromString(p.SceneValue, &number); err == nil {
		return validSceneID(number)
	}
	var pair struct {
		ParamID *int64 `json:"paramId"`
		ID      *int64 `json:"id"`
	}
	err := codec.UnmarshalFromString(p.SceneValue, &pair)
	return err == nil && validSceneID(pair.ParamID) && validSceneID(pair.ID)
}

func validSceneID(id *int64) bool {
	return id != nil && *id >= 0 && *id <= maxSafeSceneID
}

func checkLightsWhite(c chk, p LightsAction) {
	if p.Kelvin == 0 {
		c.add("kelvin", CodeRequired)
		return
	}
	inRange(c, "kelvin", p.Kelvin, between(MinLightsKelvin, MaxLightsKelvin))
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
	then := c.sub("then").nest()
	then.perm = max(c.perm, permRank([]Condition{p.Condition}))
	then.actions(p.Then)
	c.sub("else").nest().actions(p.Else)
}

func ifBranches(p IfAction) nesting {
	return nesting{lists: [][]Action{p.Then, p.Else}, labels: []string{"then", "else"}, condition: &p.Condition}
}
