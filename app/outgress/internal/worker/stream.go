// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// channelUpdateMeta is the stream-editor job sesame threads on a
// TypeChannelUpdate message. Field is title/game/tags; an empty Value is a
// Get Channel Information read, a non-empty Value is a PATCH. Locale and
// User compose the chat reply — only this worker sees the Helix response
// (the resolved category name, the stored tags).
type channelUpdateMeta struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Locale string `json:"locale"`
	User   string `json:"user"`
}

// streamJobMeta is the superset blob for the two single-call stream-editor
// jobs: a marker send fills Description, a commercial send fills Length.
// One struct instead of two so both jobs can share one handler body.
type streamJobMeta struct {
	Description string `json:"description"`
	Length      int    `json:"length"`
	Locale      string `json:"locale"`
	User        string `json:"user"`
}

// processChannelUpdate handles !title/!game/!tags. GET (empty value) runs
// under the app token so it works offline and without extra grants; PATCH
// (and the category search that precedes a game set) needs the broadcaster
// grant channel:manage:broadcast. Game names are resolved here because
// Helix Modify Channel takes a game_id, not a name.
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
	// PATCH succeeded. A reply failure must not redeliver: title/game/tags
	// PATCH is close to idempotent, but a second run still costs a Helix
	// write and a second chat line. Same ack-after-success shape as clip.
	if err := w.sendBotLine(ctx, payload.BroadcasterID, streamSetReply(meta, display)); err != nil {
		w.log.Warn("channel updated but reply chat failed",
			zap.String("broadcaster_id", payload.BroadcasterID),
			zap.String("field", meta.Field),
			zap.Error(err))
	}
	return nil
}

// buildChannelPatch maps field+value onto the Helix patch and the display
// string for the chat reply. ok=false with a nil error means the job is
// finished without a PATCH — chat was already answered (unknown game) or the
// field is unroutable.
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

// resolveGame turns a typed game name into the Helix category. SearchCategory
// runs on the app token — a second Helix call the broadcaster-bucket slot
// from processChannelUpdate does not cover — so the app bucket is paid here
// first; nothing was written yet, so an error can safely redeliver. A miss
// answers chat directly and reports found=false.
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

// decodeStreamMeta tolerates an absent payload: every stream-editor field has
// a workable zero value, and sesame always sends the blob anyway.
func decodeStreamMeta(payload *outgress.Message, meta any) {
	if len(payload.Payload) > 0 {
		_ = codec.Unmarshal(payload.Payload, meta)
	}
}

// runStreamJob is the single body behind marker and commercial: take the
// Helix slot as the broadcaster, run the one Helix write, then confirm in
// chat under stream.<kind>.ok. A reply failure after a successful call logs
// instead of redelivering — the Helix write already happened, so a retry
// would run it twice.
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

// streamHelixErr classifies a typed Helix/token error: 429 and 5xx (and
// transport) redeliver; a 4xx, a dead grant, or a missing broadcaster token
// cannot succeed on retry, so we ack and tell chat the update did not land.
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

// streamDrop is the ack-and-tell-chat set: a 4xx Helix rejection (including
// 401/403 the client already retried), a dead grant, or no stored broadcaster
// token. isPermanent skips 401 because EventSub treats it as "refresh may
// recover"; here the typed client already retried once, so looping just
// poisons the lane.
func streamDrop(err error) bool {
	if isPermanent(err) || errors.Is(err, twitch.ErrNoUserToken) || twitch.GrantDead(err) {
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

// splitStreamTags trims a comma-separated list the same way sesame already
// validated it. Empty pieces are dropped so a trailing comma cannot mint a
// blank tag Helix would 400.
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
