// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

type clipMeta struct {
	Title    string  `json:"title"`
	Clipper  string  `json:"clipper"`
	Duration float64 `json:"duration"`
	Reply    string  `json:"reply"`

	BroadcasterID string `json:"-"`
}

type clipCreateReply struct {
	Data []struct {
		ID      string `json:"id"`
		EditURL string `json:"edit_url"`
	} `json:"data"`
}

func (w *Worker) processClip(ctx context.Context, payload *outgress.Message) error {
	var meta clipMeta
	if len(payload.Payload) > 0 {
		_ = codec.Unmarshal(payload.Payload, &meta)
	}
	meta.BroadcasterID = payload.BroadcasterID

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err
	}

	res, err := w.callTwitch(ctx, twitch.ParseIdentity(outgress.AsBroadcaster), meta.BroadcasterID,
		twitch.HelixCall{Method: http.MethodPost, Endpoint: clipEndpoint(meta)})
	if err != nil {
		w.log.Error("clip create failed",
			zap.String("broadcaster_id", meta.BroadcasterID), zap.Error(err))
		return err
	}
	defer drainResponse(res)

	created, err := w.clipCreated(ctx, meta.BroadcasterID, res)
	if !created {
		return err
	}

	w.replyWithClip(ctx, meta, res)
	return nil
}

func clipEndpoint(meta clipMeta) string {
	q := url.Values{}
	q.Set("broadcaster_id", meta.BroadcasterID)
	if title := strings.TrimSpace(meta.Title); title != "" {
		q.Set("title", title)
	}
	if meta.Duration > 0 {
		q.Set("duration", strconv.FormatFloat(meta.Duration, 'f', -1, 64))
	}
	return "/helix/clips?" + q.Encode()
}

func (w *Worker) clipCreated(ctx context.Context, broadcasterID string, res *http.Response) (bool, error) {
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		w.log.Warn("twitch rate limited clip create",
			zap.String("broadcaster_id", broadcasterID),
			zap.Duration("retry_after", twitch.RetryAfter(res)))
		return false, fmt.Errorf("twitch 429 on clip create")

	case res.StatusCode >= 500:
		return false, fmt.Errorf("twitch server error on clip create: %d", res.StatusCode)

	case res.StatusCode >= 400:
		body := readErrorBody(res)
		w.log.Error("dropping clip: twitch rejected create",
			zap.Int("status", res.StatusCode),
			zap.String("broadcaster_id", broadcasterID),
			zap.String("body", body))
		noticeError(ctx, fmt.Errorf("twitch rejected clip create: %d %s", res.StatusCode, body))
		return false, nil
	}

	return true, nil
}

func (w *Worker) replyWithClip(ctx context.Context, meta clipMeta, res *http.Response) {
	id, err := clipID(res.Body)
	if err != nil || id == "" {
		w.log.Warn("clip created but response unparseable; skipping reply",
			zap.String("broadcaster_id", meta.BroadcasterID), zap.Error(err))
		return
	}

	clipURL := "https://clips.twitch.tv/" + id
	if err := w.sendClipReply(ctx, meta, clipURL); err != nil {
		w.log.Warn("clip created but reply chat failed",
			zap.String("broadcaster_id", meta.BroadcasterID), zap.Error(err))
		return
	}

	evt := data.ClipCreated{
		BroadcasterID: meta.BroadcasterID,
		ClipID:        id,
		URL:           clipURL,
		Clipper:       meta.Clipper,
		Title:         strings.TrimSpace(meta.Title),
	}
	w.publishClipCreated(ctx, evt)
	w.scheduleClipVerify(evt.BroadcasterID, evt.Clipper, evt.ClipID)
}

func (w *Worker) publishClipCreated(ctx context.Context, evt data.ClipCreated) {
	if w.factPub == nil {
		return
	}
	if err := bus.PublishJSON(ctx, w.factPub, data.SubjectClipCreated, evt); err != nil {
		w.log.Warn("failed to publish clip created fact",
			zap.String("broadcaster_id", evt.BroadcasterID), zap.Error(err))
	}
}

func clipID(body io.Reader) (string, error) {
	var reply clipCreateReply
	if err := codec.NewDecoder(io.LimitReader(body, 4096)).Decode(&reply); err != nil {
		return "", err
	}
	if len(reply.Data) == 0 {
		return "", nil
	}
	return reply.Data[0].ID, nil
}

func (w *Worker) sendClipReply(ctx context.Context, meta clipMeta, clipURL string) error {
	return w.sendBotLine(ctx, meta.BroadcasterID, clipReplyText(meta, clipURL))
}

func clipReplyText(meta clipMeta, clipURL string) string {
	who := meta.Clipper
	title := strings.TrimSpace(meta.Title)
	if strings.TrimSpace(meta.Reply) != "" {
		return clipExpand(meta, clipURL)
	}
	switch {
	case who != "" && title != "":
		return who + " clipped: " + title + " → " + clipURL
	case who != "":
		return who + " made a clip → " + clipURL
	case title != "":
		return "Clip: " + title + " → " + clipURL
	default:
		return "New clip → " + clipURL
	}
}

func clipExpand(meta clipMeta, clipURL string) string {
	// Strip leading slashes so a viewer-typed title cannot become a slash-verb as the bot.
	title := strings.TrimLeft(strings.TrimSpace(meta.Title), " /")
	tokens := map[string]string{
		"clip":    clipURL,
		"user":    meta.Clipper,
		"clipper": meta.Clipper,
		"target":  title,
		"title":   title,
	}
	return expandTokens(strings.TrimSpace(meta.Reply), tokens)
}

func expandTokens(t string, tokens map[string]string) string {
	return tmpl.Expand(t, func(tok tmpl.Token) (string, bool) {
		if val, ok := tokens[tok.Key()]; ok {
			return val, true
		}
		return tmpl.Dynamic(tok)
	})
}
