// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"
)

type banData struct {
	UserID   string `json:"user_id"`
	Duration int    `json:"duration,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func buildOutgress(o *module.Output) ([]byte, error) {
	msg, err := buildOutgressMessage(o)
	if err != nil {
		return nil, err
	}
	return codec.Marshal(&msg)
}

func buildOutgressMessage(o *module.Output) (outgress.Message, error) {
	if o.Type == outgress.TypeBatch {
		items := make([]outgress.Message, 0, len(o.Items))
		for i := range o.Items {
			item, err := buildOutgressMessage(&o.Items[i])
			if err != nil {
				return outgress.Message{}, err
			}
			items = append(items, item)
		}
		return payloadMessage(outgress.TypeBatch, o.BroadcasterID, &outgress.Batch{ID: o.BatchID, Items: items})
	}

	build, ok := outgressBuilders[o.Type]
	if !ok {
		return outgress.Message{
			Type:          o.Type,
			BroadcasterID: o.BroadcasterID,
		}, nil
	}
	msg, err := build(o)
	if err != nil {
		return outgress.Message{}, err
	}
	msg.Locale = o.Locale
	return msg, nil
}

var outgressBuilders = map[string]func(*module.Output) (outgress.Message, error){
	outgress.TypeChat:             chatOutgress,
	outgress.TypeAnnounce:         announceOutgress,
	outgress.TypeShoutout:         shoutoutOutgress,
	outgress.TypePin:              pinOutgress,
	outgress.TypeClip:             clipOutgress,
	outgress.TypeChannelUpdate:    channelUpdateOutgress,
	outgress.TypeStreamMarker:     streamMarkerOutgress,
	outgress.TypeCommercial:       commercialOutgress,
	outgress.TypeBan:              banOutgress,
	outgress.TypeTimeout:          banOutgress,
	outgress.TypeShieldMode:       shieldOutgress,
	outgress.TypeDelete:           deleteOutgress,
	outgress.TypeWarn:             warnOutgress,
	outgress.TypeRedemptionUpdate: redemptionUpdateOutgress,
}

func payloadMessage(msgType, broadcasterID string, payload any) (outgress.Message, error) {
	body, err := codec.Marshal(payload)
	if err != nil {
		return outgress.Message{}, err
	}
	return outgress.Message{
		Type:          msgType,
		BroadcasterID: broadcasterID,
		Payload:       body,
	}, nil
}

func chatOutgress(o *module.Output) (outgress.Message, error) {
	return textOutgress(outgress.TypeChat, o)
}

func pinOutgress(o *module.Output) (outgress.Message, error) {
	return textOutgress(outgress.TypePin, o)
}

func textOutgress(msgType string, o *module.Output) (outgress.Message, error) {
	return payloadMessage(msgType, o.BroadcasterID, &struct {
		BroadcasterID string `json:"broadcaster_id"`
		Message       string `json:"message"`
	}{o.BroadcasterID, o.Text})
}

func announceOutgress(o *module.Output) (outgress.Message, error) {
	msg, err := payloadMessage(outgress.TypeAnnounce, o.BroadcasterID, &struct {
		Message string `json:"message"`
	}{o.Text})
	msg.Color = o.Color
	return msg, err
}

func shoutoutOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeShoutout,
		BroadcasterID: o.BroadcasterID,
		To:            o.To,
		Payload:       []byte("{}"),
	}, nil
}

func clipOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeClip, o.BroadcasterID, &struct {
		Title    string  `json:"title,omitempty"`
		Clipper  string  `json:"clipper,omitempty"`
		Duration float64 `json:"duration,omitempty"`
		Reply    string  `json:"reply,omitempty"`
	}{o.Text, o.To, o.Duration, o.Template})
}

func channelUpdateOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeChannelUpdate, o.BroadcasterID, &struct {
		Field  string `json:"field"`
		Value  string `json:"value,omitempty"`
		Locale string `json:"locale,omitempty"`
		User   string `json:"user,omitempty"`
	}{o.Reason, o.Text, o.Template, o.To})
}

func streamMarkerOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeStreamMarker, o.BroadcasterID, &struct {
		Description string `json:"description,omitempty"`
		Locale      string `json:"locale,omitempty"`
		User        string `json:"user,omitempty"`
	}{o.Text, o.Template, o.To})
}

func commercialOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeCommercial, o.BroadcasterID, &struct {
		Length int    `json:"length"`
		Locale string `json:"locale,omitempty"`
		User   string `json:"user,omitempty"`
	}{int(o.Duration), o.Template, o.To})
}

func banOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(o.Type, o.BroadcasterID, &struct {
		Data banData `json:"data"`
	}{banData{UserID: o.TargetUserID, Duration: int(o.Duration), Reason: o.Reason}})
}

func shieldOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeShieldMode,
		BroadcasterID: o.BroadcasterID,
		Payload:       []byte(`{"is_active":true}`),
	}, nil
}

func deleteOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeDelete,
		BroadcasterID: o.BroadcasterID,
		MsgID:         o.MsgID,
	}, nil
}

func warnOutgress(o *module.Output) (outgress.Message, error) {
	return payloadMessage(outgress.TypeWarn, o.BroadcasterID, &struct {
		Data banData `json:"data"`
	}{banData{UserID: o.TargetUserID, Reason: o.Reason}})
}

func redemptionUpdateOutgress(o *module.Output) (outgress.Message, error) {
	return outgress.Message{
		Type:          outgress.TypeRedemptionUpdate,
		BroadcasterID: o.BroadcasterID,
		RewardID:      o.RewardID,
		RedemptionID:  o.RedemptionID,
		Status:        o.Status,
	}, nil
}
