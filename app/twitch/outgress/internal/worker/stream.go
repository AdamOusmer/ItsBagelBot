// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type channelUpdateMeta struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Locale string `json:"locale"`
	User   string `json:"user"`
}

type streamJobMeta struct {
	Description string `json:"description"`
	Length      int    `json:"length"`
	Locale      string `json:"locale"`
	User        string `json:"user"`
}

func (w *Worker) processChannelUpdate(ctx context.Context, payload *outgress.Message) error {
	var meta channelUpdateMeta
	decodeStreamMeta(payload, &meta)
	if payload.BroadcasterID == "" {
		return nil
	}

	if meta.Value == "" {
		payload.As = outgress.AsApp
	} else {
		payload.As = outgress.AsBroadcaster
	}
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	if meta.Value == "" {
		return w.streamGet(ctx, payload, meta)
	}
	return w.streamSet(ctx, payload, meta)
}

func (w *Worker) streamGet(ctx context.Context, payload *outgress.Message, meta channelUpdateMeta) error {
	info, err := w.twitch.ChannelInfo(ctx, payload.BroadcasterID)
	if err != nil {
		return w.streamHelixErr(ctx, payload, meta.Locale, err)
	}
	return w.sendBotLine(ctx, payload.BroadcasterID, streamGetReply(meta, info))
}

func (w *Worker) streamSet(ctx context.Context, payload *outgress.Message, meta channelUpdateMeta) error {
	patch, display, ok, err := w.buildChannelPatch(ctx, payload, meta)
	if err != nil {
		return w.streamHelixErr(ctx, payload, meta.Locale, err)
	}
	if !ok {
		return nil
	}

	if err := w.twitch.ModifyChannel(ctx, payload.BroadcasterID, patch); err != nil {
		return w.streamHelixErr(ctx, payload, meta.Locale, err)
	}
	if err := w.sendBotLine(ctx, payload.BroadcasterID, streamSetReply(meta, display)); err != nil {
		w.log.Warn("channel updated but reply chat failed",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("field", meta.Field),
			zap.Error(err))
	}
	return nil
}

func (w *Worker) buildChannelPatch(ctx context.Context, payload *outgress.Message, meta channelUpdateMeta) (patch twitch.ChannelPatch, display string, ok bool, err error) {
	display = strings.TrimSpace(meta.Value)
	switch meta.Field {
	case "title":
		patch.Title = display
	case "game":
		cat, found, err := w.resolveGame(ctx, payload, meta, display)
		if err != nil || !found {
			return patch, display, false, err
		}
		patch.GameID = cat.ID
		display = cat.Name
	case "tags":
		patch.Tags = splitStreamTags(meta.Value)
		display = strings.Join(patch.Tags, ", ")
	default:
		w.log.Error("dropping channel_update with unknown field",
			zap.String("field", meta.Field),
			zap.String("broadcaster_id", payload.BroadcasterID))
		return patch, display, false, nil
	}
	return patch, display, true, nil
}

func (w *Worker) resolveGame(ctx context.Context, payload *outgress.Message, meta channelUpdateMeta, name string) (cat twitch.Category, found bool, err error) {
	if err := w.takeAppHelix(ctx); err != nil {
		return cat, false, err
	}
	cat, found, err = w.twitch.SearchCategory(ctx, name)
	if err != nil || found {
		return cat, found, err
	}
	_ = w.sendBotLine(ctx, payload.BroadcasterID, streamExpand(meta.Locale, "stream.game.not_found", map[string]string{
		"user": meta.User,
		"game": name,
	}))
	return cat, false, nil
}

func (w *Worker) processMarker(ctx context.Context, payload *outgress.Message) error {
	return w.runStreamJob(ctx, payload, "marker")
}

func (w *Worker) processCommercial(ctx context.Context, payload *outgress.Message) error {
	return w.runStreamJob(ctx, payload, "commercial")
}

func decodeStreamMeta(payload *outgress.Message, meta any) {
	if len(payload.Payload) > 0 {
		_ = codec.Unmarshal(payload.Payload, meta)
	}
}

func (w *Worker) runStreamJob(ctx context.Context, payload *outgress.Message, kind string) error {
	var meta streamJobMeta
	decodeStreamMeta(payload, &meta)
	call := func() error { return w.twitch.CreateMarker(ctx, payload.BroadcasterID, meta.Description) }
	tokens := map[string]string{"user": meta.User}
	if kind == "commercial" {
		call = func() error { return w.twitch.StartCommercial(ctx, payload.BroadcasterID, meta.Length) }
		tokens["length"] = strconv.Itoa(meta.Length)
	}

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}
	if err := call(); err != nil {
		return w.streamHelixErr(ctx, payload, meta.Locale, err)
	}
	if err := w.sendBotLine(ctx, payload.BroadcasterID, streamExpand(meta.Locale, "stream."+kind+".ok", tokens)); err != nil {
		w.log.Warn("stream job ran but reply chat failed",
			zap.String("kind", kind),
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
	}
	return nil
}

func (w *Worker) streamHelixErr(ctx context.Context, payload *outgress.Message, locale string, err error) error {
	if err == nil {
		return nil
	}
	if streamDrop(err) {
		w.log.Error("dropping stream-editor job: twitch rejected it",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("type", payload.Type),
			zap.Error(err))
		_ = w.sendBotLine(ctx, payload.BroadcasterID, streamExpand(locale, "stream.unavailable", nil))
		return nil
	}
	return err
}

func streamDrop(err error) bool {
	if isPermanent(err) || twitch.GrantDead(err) {
		return true
	}
	if errors.Is(err, twitch.ErrNoUserToken) {
		return true
	}
	var se *twitch.StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusUnauthorized || se.Status == http.StatusForbidden
	}
	return false
}

func streamGetReply(meta channelUpdateMeta, info twitch.ChannelInfo) string {
	switch meta.Field {
	case "title":
		return streamExpand(meta.Locale, "stream.title.current", map[string]string{"title": info.Title})
	case "game":
		return streamExpand(meta.Locale, "stream.game.current", map[string]string{"game": info.GameName})
	case "tags":
		if len(info.Tags) == 0 {
			return streamExpand(meta.Locale, "stream.tags.none", nil)
		}
		return streamExpand(meta.Locale, "stream.tags.current", map[string]string{"tags": strings.Join(info.Tags, ", ")})
	default:
		return streamExpand(meta.Locale, "stream.unavailable", nil)
	}
}

func streamSetReply(meta channelUpdateMeta, value string) string {
	key := "stream." + meta.Field + ".updated"
	tokens := map[string]string{"user": meta.User}
	switch meta.Field {
	case "title":
		tokens["title"] = value
	case "game":
		tokens["game"] = value
	case "tags":
		tokens["tags"] = value
	}
	return streamExpand(meta.Locale, key, tokens)
}

func streamExpand(locale, key string, tokens map[string]string) string {
	tmpl := i18n.T(locale, key)
	if len(tokens) == 0 {
		return tmpl
	}
	return expandTokens(tmpl, tokens)
}

func splitStreamTags(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		tag := strings.TrimSpace(p)
		if tag != "" {
			out = append(out, tag)
		}
	}
	return out
}
