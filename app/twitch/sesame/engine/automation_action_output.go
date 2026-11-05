// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"strconv"
	"strings"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

const (
	maxMarkerRunes = 140
	maxTitleRunes  = 140
	maxGameRunes   = 100
	maxTagsRunes   = 500
	rewardQueued   = "queued"
)

func (x *actionExec) chat(_ step, p automation.TextAction) error {
	o := x.out(outgress.TypeChat)
	o.Text = x.expand(p.Text)
	return x.queueText(o)
}

func (x *actionExec) announce(_ step, p automation.AnnounceAction) error {
	o := x.out(outgress.TypeAnnounce)
	o.Text, o.Color = x.expand(p.Text), p.Color
	return x.queueText(o)
}

func (x *actionExec) reply(_ step, p automation.TextAction) error {
	o := x.out(outgress.TypeChat)
	o.Text = x.expand(p.Text)
	if name := x.viewerName(); name != "" && strings.TrimSpace(o.Text) != "" {
		o.Text = "@" + name + " " + o.Text
	}
	return x.queueText(o)
}

func (x *actionExec) viewerName() string {
	v := x.run.Viewer
	if v.Name != "" {
		return strings.TrimPrefix(v.Name, "@")
	}
	return strings.TrimPrefix(v.Login, "@")
}

func (x *actionExec) timeout(_ step, p automation.TimeoutAction) error {
	return x.moderate(outgress.TypeTimeout, p.Login, p.Reason, float64(p.Seconds))
}

func (x *actionExec) ban(_ step, p automation.BanAction) error {
	return x.moderate(outgress.TypeBan, p.Login, p.Reason, 0)
}

func (x *actionExec) warn(_ step, p automation.WarnAction) error {
	return x.moderate(outgress.TypeWarn, p.Login, p.Reason, 0)
}

func (x *actionExec) moderate(typ, login, reason string, seconds float64) error {
	t, err := x.target(login, false)
	if err != nil {
		return err
	}
	o := x.out(typ)
	o.TargetUserID = strconv.FormatUint(t.id, 10)
	o.Duration, o.Reason = seconds, x.expand(reason)
	return x.queue(o)
}

func (x *actionExec) deleteMessage(_ step, _ automation.EmptyParams) error {
	if x.run.Env == nil || x.run.Env.MsgID == "" {
		return failure("no_message")
	}
	o := x.out(outgress.TypeDelete)
	o.MsgID = x.run.Env.MsgID
	return x.queue(o)
}

func (x *actionExec) shield(_ step, p automation.ShieldAction) error {
	o := x.out(outgress.TypeShieldMode)
	if !p.On {
		o.Status = shieldStatusOff
	}
	return x.queue(o)
}

func (x *actionExec) shoutout(_ step, p automation.LoginAction) error {
	o := x.out(outgress.TypeShoutout)
	o.To = normalizeLogin(x.expand(p.Login))
	return x.queue(o)
}

func (x *actionExec) clip(_ step, _ automation.EmptyParams) error {
	o := x.out(outgress.TypeClip)
	o.To = x.viewerName()
	return x.queue(o)
}

func (x *actionExec) marker(_ step, p automation.MarkerAction) error {
	o := x.out(outgress.TypeStreamMarker)
	o.Text, o.Template, o.To = truncateRunes(strings.TrimSpace(x.expand(p.Description)), maxMarkerRunes), x.context().Locale, x.viewerName()
	return x.queue(o)
}

func (x *actionExec) setTitle(_ step, p automation.SetTitleAction) error {
	return x.channelUpdate("title", p.Title, maxTitleRunes)
}

func (x *actionExec) setGame(_ step, p automation.SetGameAction) error {
	return x.channelUpdate("game", p.Game, maxGameRunes)
}

func (x *actionExec) setTags(_ step, p automation.SetTagsAction) error {
	return x.channelUpdate("tags", p.Tags, maxTagsRunes)
}

func (x *actionExec) channelUpdate(field, template string, maxRunes int) error {
	value := strings.TrimSpace(x.expand(template))
	switch {
	case value == "":
		return failure("empty")
	case len([]rune(value)) > maxRunes:
		return failure("too_long")
	}
	o := x.out(outgress.TypeChannelUpdate)
	o.Text, o.Reason, o.Template, o.To = value, field, x.context().Locale, x.viewerName()
	return x.queue(o)
}

func (x *actionExec) commercial(_ step, p automation.CommercialAction) error {
	o := x.out(outgress.TypeCommercial)
	o.Duration, o.Template, o.To = float64(p.Seconds), x.context().Locale, x.viewerName()
	return x.queue(o)
}

var redemptionStatuses = map[string]string{
	"fulfill": outgress.RedemptionFulfilled,
	"cancel":  outgress.RedemptionCanceled,
}

func (x *actionExec) redemption(_ step, p automation.RedemptionAction) error {
	rewardID, redemptionID := x.redemptionIDs()
	status, known := redemptionStatuses[p.Status]
	if rewardID == "" || redemptionID == "" {
		return failure("no_redemption")
	}
	if !known {
		return failure("no_redemption")
	}
	o := x.out(outgress.TypeRedemptionUpdate)
	o.RewardID, o.RedemptionID, o.Status = rewardID, redemptionID, status
	return x.queue(o)
}

func (x *actionExec) rewardUpdate(s step, p automation.RewardUpdateAction) error {
	o := x.out(outgress.TypeRewardUpdate)
	o.RewardID = p.RewardID
	o.Reward = &outgress.RewardUpdate{Enabled: p.Enabled, Cost: p.Cost, ResetOnStreamEnd: p.ResetOnStreamEnd}
	// Not the record's position: parallel paths and trimming both reuse positions.
	o.RunID, o.ActionSlot = x.run.ID, s.EffectKey+x.iter
	if err := x.queue(o); err != nil {
		return err
	}
	x.run.Record.Actions[len(x.run.Record.Actions)-1].Slot = o.ActionSlot
	return succeeded(rewardQueued)
}

func (x *actionExec) redemptionIDs() (rewardID, redemptionID string) {
	if x.run.Env == nil || len(x.run.Env.Event) == 0 {
		return "", ""
	}
	var ev struct {
		ID     string `json:"id"`
		Reward struct {
			ID string `json:"id"`
		} `json:"reward"`
	}
	if err := codec.Unmarshal(x.run.Env.Event, &ev); err != nil {
		return "", ""
	}
	return ev.Reward.ID, ev.ID
}
