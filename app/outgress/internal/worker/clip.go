package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"ItsBagelBot/app/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/bytedance/sonic"

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
//
// Redelivery safety: once the clip is created (2xx) this returns nil no matter
// what happens to the reply — re-running the message would create a DUPLICATE
// clip, far worse than a missing reply line. Only failures BEFORE the clip
// exists (rate bucket, transport, 429, 5xx) return an error to redeliver.
func (w *Worker) processClip(ctx context.Context, payload *outgress.Message) error {
	var meta clipMeta
	if len(payload.Payload) > 0 {
		_ = sonic.Unmarshal(payload.Payload, &meta)
	}

	payload.As = outgress.AsBroadcaster
	if err := w.takeGeneralHelix(ctx, payload); err != nil {
		return err // no clip created yet: safe to redeliver
	}

	res, err := w.callTwitch(ctx, twitch.ParseIdentity(outgress.AsBroadcaster), payload.BroadcasterID,
		twitch.HelixCall{Method: http.MethodPost, Endpoint: clipEndpoint(payload.BroadcasterID, meta)})
	if err != nil {
		w.log.Error("clip create failed",
			zap.String("broadcaster_id", payload.BroadcasterID), zap.Error(err))
		return err // no clip: redeliver
	}
	defer drainResponse(res)

	created, err := w.clipCreated(ctx, payload.BroadcasterID, res)
	if !created {
		return err
	}

	// Clip now exists. From here on never return an error (see the doc comment).
	w.replyWithClip(ctx, payload.BroadcasterID, meta, res)
	return nil
}

// clipEndpoint assembles the Create Clip path: broadcaster_id, and the
// optional title and duration, all ride the query string; the call takes no
// body. Duration 0 is omitted so Twitch applies its default length.
func clipEndpoint(broadcasterID string, meta clipMeta) string {
	q := url.Values{}
	q.Set("broadcaster_id", broadcasterID)
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
// log; the caller acks regardless.
func (w *Worker) replyWithClip(ctx context.Context, broadcasterID string, meta clipMeta, res *http.Response) {
	id, err := clipID(res.Body)
	if err != nil || id == "" {
		w.log.Warn("clip created but response unparseable; skipping reply",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}

	clipURL := "https://clips.twitch.tv/" + id
	if err := w.sendClipReply(ctx, broadcasterID, meta, clipURL); err != nil {
		w.log.Warn("clip created but reply chat failed",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// clipID decodes the Create Clip response body and returns the new clip's id
// ("" when the response carries none).
func clipID(body io.Reader) (string, error) {
	var reply clipCreateReply
	if err := json.NewDecoder(io.LimitReader(body, 4096)).Decode(&reply); err != nil {
		return "", err
	}
	if len(reply.Data) == 0 {
		return "", nil
	}
	return reply.Data[0].ID, nil
}

// sendClipReply posts the chat line announcing a freshly created clip through
// the normal chat action (registry route, rate buckets, sender-id injection).
// Its error is only for the caller to log; the clip already exists, so the
// caller must not redeliver on a reply failure.
func (w *Worker) sendClipReply(ctx context.Context, broadcasterID string, meta clipMeta, clipURL string) error {
	return w.sendBotChat(ctx, broadcasterID, clipReplyText(meta, clipURL))
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
	title := strings.TrimSpace(meta.Title)
	return strings.NewReplacer(
		"{clip}", clipURL,
		"{user}", meta.Clipper,
		"{clipper}", meta.Clipper,
		"{target}", title,
		"{title}", title,
	).Replace(strings.TrimSpace(meta.Reply))
}
