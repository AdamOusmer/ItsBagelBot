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

	"go.uber.org/zap"
)

// clipMeta is the metadata sesame threads on a TypeClip message: the title the
// viewer typed, their login, the requested clip length, and the broadcaster's
// custom reply template. Title and Duration are passed through to Twitch's
// Create Clip call (both in the query string); Title, Clipper and Reply compose
// the chat reply posted with the clip URL. Duration 0 means unset, so Twitch
// applies its default (30s); an empty Reply falls back to the default format.
type clipMeta struct {
	Title    string  `json:"title"`
	Clipper  string  `json:"clipper"`
	Duration float64 `json:"duration"`
	Reply    string  `json:"reply"`

	// BroadcasterID is not part of the sesame wire payload (TypeClip never
	// sends a broadcaster_id key): processClip fills it in from
	// payload.BroadcasterID right after unmarshal so every downstream clip
	// helper threads one struct instead of a bare broadcasterID string
	// running alongside meta as a repeated pair of arguments.
	BroadcasterID string `json:"-"`
}

// clipCreateReply is the subset of the Helix Create Clip response we read.
type clipCreateReply struct {
	Data []struct {
		ID      string `json:"id"`
		EditURL string `json:"edit_url"`
	} `json:"data"`
}

// processClip creates a clip on the broadcaster's channel and posts the public
// clip URL back to chat. The Create Clip response (and thus the URL) is visible
// only here, so this is the one place that can surface it.
//
// The reply posts immediately with the constructed public URL
// (https://clips.twitch.tv/<id>): the Create Clip id doubles as the clip's
// public slug, and Get Clips reports exactly that link once processing
// finishes, so polling it first only delayed the reply by seconds while
// pinning a lane routine — the link resolves the moment Twitch publishes.
// Create Clip is async, though, and a clip can die in processing AFTER the
// 2xx, leaving the posted link permanently dead with no error to us. A
// detached background check (scheduleClipVerify) polls Get Clips past the
// publication window and posts a follow-up notice on confirmed absence.
//
// Redelivery safety: once the clip is created (2xx) this returns nil no matter
// what happens to the reply — re-running the message would create a DUPLICATE
// clip, far worse than a missing reply line. Only failures BEFORE the clip
// exists (rate bucket, transport, 429, 5xx) return an error to redeliver.
func (w *Worker) processClip(ctx context.Context, payload *outgress.Message) error {
	var meta clipMeta
	if len(payload.Payload) > 0 {
		_ = codec.Unmarshal(payload.Payload, &meta)
	}
	meta.BroadcasterID = payload.BroadcasterID

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err // no clip created yet: safe to redeliver
	}

	res, err := w.callTwitch(ctx, twitch.ParseIdentity(outgress.AsBroadcaster), meta.BroadcasterID,
		twitch.HelixCall{Method: http.MethodPost, Endpoint: clipEndpoint(meta)})
	if err != nil {
		w.log.Error("clip create failed",
			zap.String("broadcaster_id", meta.BroadcasterID), zap.Error(err))
		return err // no clip: redeliver
	}
	defer drainResponse(res)

	created, err := w.clipCreated(ctx, meta.BroadcasterID, res)
	if !created {
		return err
	}

	// Clip now exists. From here on never return an error (see the doc comment).
	w.replyWithClip(ctx, meta, res)
	return nil
}

// clipEndpoint assembles the Create Clip path: broadcaster_id, and the
// optional title and duration, all ride the query string; the call takes no
// body. Duration 0 is omitted so Twitch applies its default length.
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

// clipCreated classifies the Create Clip response: (true, nil) once the clip
// exists, (false, err) for retryables (429, 5xx), and (false, nil) for
// permanent rejections that must not redeliver.
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

// replyWithClip reads the created clip's id off the response and posts the
// public URL back to chat. The clip already exists, so failures here only
// log; the caller acks regardless. A posted reply also arms the background
// publication check: only then is there a link in chat that could go dead.
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

	// Built once here, after id/clipURL both exist, and passed whole from
	// here on: publishClipCreated used to take broadcasterID, clipID,
	// clipURL and meta as four separate strings/struct in the same order
	// replyWithClip already holds them in, which is exactly what
	// data.ClipCreated already models.
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

// publishClipCreated announces the clip as a fact on BAGEL_DATA rather than
// posting a Discord embed directly: see data.SubjectClipCreated's doc comment
// for why this has to be a fact and not a discord_chat command -- in short, a
// work-queue Discord lane could only ever hand the clip to one subscriber,
// and outgress would have to learn Discord exists (module blob, embed
// builder, enabled check) just to make an announcement someone else cares
// about. Best-effort, exactly like the Discord post it replaces: the clip
// already exists and the chat reply already went out, so a failed publish
// only logs and moves on.
func (w *Worker) publishClipCreated(ctx context.Context, evt data.ClipCreated) {
	if w.factPub == nil {
		return
	}
	// Warn, not Debug: best-effort means the clip still exists and chat was
	// still answered, NOT that the loss is uninteresting. This publish is the
	// only signal any subscriber gets, so a dropped one is a silently missing
	// archive post with nothing downstream able to notice -- the same reason
	// the Discord path this replaced logged its skipped posts rather than
	// swallowing them.
	if err := bus.PublishJSON(ctx, w.factPub, data.SubjectClipCreated, evt); err != nil {
		w.log.Warn("failed to publish clip created fact",
			zap.String("broadcaster_id", evt.BroadcasterID), zap.Error(err))
	}
}

// clipID decodes the Create Clip response body and returns the new clip's id
// ("" when the response carries none).
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

// sendClipReply posts the chat line announcing a freshly created clip through
// the normal actions (registry route, rate buckets, sender-id injection). The
// reply goes through sendBotLine, so a custom template leading with a
// slash-verb (/announce, /pin, …) becomes that native action, exactly like
// every sesame-emitted reply. Its error is only for the caller to log; the
// clip already exists, so the caller must not redeliver on a reply failure.
func (w *Worker) sendClipReply(ctx context.Context, meta clipMeta, clipURL string) error {
	return w.sendBotLine(ctx, meta.BroadcasterID, clipReplyText(meta, clipURL))
}

// clipReplyText composes the chat line for a new clip. When the broadcaster set
// a custom reply template it is expanded (see clipExpand); otherwise a default
// line is used that names the clipper, echoes the title they typed (when any),
// and links the public clip URL.
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

// clipExpand substitutes the clip reply tokens into the broadcaster's custom
// template (meta.Reply): {clip} → the public clip URL, {user}/{clipper} → the
// clipper's login, {target}/{title} → the title the viewer typed. Unknown
// tokens are left untouched (mirroring the dashboard rehearsal, which marks
// them). The {user} and {target} aliases match the standard command tokens so
// the same palette applies; {clipper}/{title} read more naturally for a clip.
func clipExpand(meta clipMeta, clipURL string) string {
	// The title is viewer-typed (!clip <title>). The reply now routes leading
	// slash-verbs, so a template starting with {title}/{target} must not let
	// a viewer mint /announce as the bot: strip a leading slash/space run,
	// mirroring sesame's sanitizeVar for command {args}.
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

// expandTokens is a single-pass {key} substitution mirroring sesame's
// module.Expand semantics (outgress does not import sesame packages): token
// names are case-insensitive, an unknown token stays literal (braces and
// all), and a '{' with no closing brace is copied through to the end.
func expandTokens(tmpl string, tokens map[string]string) string {
	var b strings.Builder
	b.Grow(len(tmpl))
	for i := 0; i < len(tmpl); {
		open := strings.IndexByte(tmpl[i:], '{')
		if open < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		open += i
		end := strings.IndexByte(tmpl[open:], '}')
		if end < 0 {
			b.WriteString(tmpl[i:])
			break
		}
		end += open
		b.WriteString(tmpl[i:open])
		if val, ok := tokens[strings.ToLower(tmpl[open+1:end])]; ok {
			b.WriteString(val)
		} else {
			b.WriteString(tmpl[open : end+1])
		}
		i = end + 1
	}
	return b.String()
}
