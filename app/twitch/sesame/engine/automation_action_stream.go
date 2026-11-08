// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
	"strings"
)

var (
	pollStatuses = map[string]string{
		"terminate": outgress.PollTerminated,
		"archive":   outgress.PollArchived,
	}
	predictionStatuses = map[string]string{
		"lock":    outgress.PredictionLocked,
		"cancel":  outgress.PredictionCanceled,
		"resolve": outgress.PredictionResolved,
	}
	unbanStatuses = map[string]string{
		"approve": outgress.UnbanApproved,
		"deny":    outgress.UnbanDenied,
	}
	chatModeNames = map[string]string{
		"emote_only":  outgress.ChatModeEmote,
		"followers":   outgress.ChatModeFollower,
		"slow":        outgress.ChatModeSlow,
		"subscribers": outgress.ChatModeSubscriber,
		"unique":      outgress.ChatModeUnique,
	}
	toggleTypes = map[string][2]string{
		"mod": {outgress.TypeModRemove, outgress.TypeModAdd},
		"vip": {outgress.TypeVIPRemove, outgress.TypeVIPAdd},
	}
)

func streamActions(b *actionTableBuilder) *actionTableBuilder {
	return b.
		Step("poll", typed((*actionExec).poll)).
		Step("poll_end", typed((*actionExec).pollEnd)).
		Step("prediction", typed((*actionExec).prediction)).
		Step("prediction_end", typed((*actionExec).predictionEnd)).
		Step("raid", typed((*actionExec).raid)).
		Step("raid_cancel", typed(bareAction(outgress.TypeRaidCancel))).
		Step("mod", typed(toggleAction("mod"))).
		Step("vip", typed(toggleAction("vip"))).
		Step("chat_mode", typed((*actionExec).chatMode)).
		Step("clear_chat", typed(bareAction(outgress.TypeClearChat))).
		Step("ad_snooze", typed(bareAction(outgress.TypeAdSnooze))).
		Step("whisper", typed((*actionExec).whisper)).
		Step("unban_resolve", typed((*actionExec).unbanResolve)).
		Step("branded_content", typed((*actionExec).brandedContent))
}

func (x *actionExec) typedOut(typ, to string, payload any) error {
	o := x.out(typ)
	o.To, o.Action = to, payload
	return x.queue(o)
}

func (x *actionExec) loginOut(typ, template string, payload any) error {
	login := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(x.expand(template)), "@"))
	if login == "" {
		return failure("empty_login")
	}
	return x.typedOut(typ, login, payload)
}

func (x *actionExec) expandAll(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = x.expand(s)
	}
	return out
}

func bareAction(typ string) func(*actionExec, step, automation.EmptyParams) error {
	return func(x *actionExec, _ step, _ automation.EmptyParams) error { return x.typedOut(typ, "", nil) }
}

func toggleAction(name string) func(*actionExec, step, automation.LoginToggleAction) error {
	return func(x *actionExec, _ step, p automation.LoginToggleAction) error {
		types := toggleTypes[name]
		typ := types[0]
		if p.On {
			typ = types[1]
		}
		return x.loginOut(typ, p.Login, nil)
	}
}

func (x *actionExec) poll(_ step, p automation.PollAction) error {
	return x.typedOut(outgress.TypePollCreate, "", outgress.PollCreate{
		Title: x.expand(p.Title), Choices: x.expandAll(p.Choices), Duration: p.Seconds,
	})
}

func (x *actionExec) pollEnd(_ step, p automation.PollEndAction) error {
	return x.typedOut(outgress.TypePollEnd, "", outgress.PollEnd{Status: pollStatuses[p.Status]})
}

func (x *actionExec) prediction(_ step, p automation.PredictionAction) error {
	return x.typedOut(outgress.TypePredictionCreate, "", outgress.PredictionCreate{
		Title: x.expand(p.Title), Outcomes: x.expandAll(p.Outcomes), Window: p.Seconds,
	})
}

func (x *actionExec) predictionEnd(_ step, p automation.PredictionEndAction) error {
	end := outgress.PredictionEnd{Status: predictionStatuses[p.Status]}
	if end.Status == outgress.PredictionResolved {
		end.Winner = p.Winner
	}
	return x.typedOut(outgress.TypePredictionEnd, "", end)
}

func (x *actionExec) raid(_ step, p automation.LoginAction) error {
	return x.loginOut(outgress.TypeRaidStart, p.Login, nil)
}

func (x *actionExec) chatMode(_ step, p automation.ChatModeAction) error {
	settings := outgress.ChatSettings{Mode: chatModeNames[p.Mode], Enabled: p.On}
	if p.On {
		settings.Duration = p.Value
	}
	return x.typedOut(outgress.TypeChatSettings, "", settings)
}

func (x *actionExec) whisper(_ step, p automation.WhisperAction) error {
	return x.loginOut(outgress.TypeWhisper, p.Login, outgress.Whisper{Message: x.expand(p.Text)})
}

func (x *actionExec) unbanResolve(_ step, p automation.UnbanResolveAction) error {
	id := x.eventID()
	if id == "" {
		return failure("no_unban_request")
	}
	return x.typedOut(outgress.TypeUnbanResolve, "", outgress.UnbanResolve{
		RequestID: id, Status: unbanStatuses[p.Status], Resolution: x.expand(p.Resolution),
	})
}

func (x *actionExec) eventID() string {
	if x.run.Env == nil || len(x.run.Env.Event) == 0 {
		return ""
	}
	var ev struct {
		ID string `json:"id"`
	}
	if err := codec.Unmarshal(x.run.Env.Event, &ev); err != nil {
		return ""
	}
	return ev.ID
}

func (x *actionExec) brandedContent(_ step, p automation.BrandedContentAction) error {
	return x.typedOut(outgress.TypeBrandedContent, "", outgress.BrandedContent{Enabled: p.On})
}
