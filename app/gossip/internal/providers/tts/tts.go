// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/ratelimit"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

const (
	bucketPrefix  = "ratelimit:gossip:{tts}:"
	defaultVoice  = "Kore"
	channelPrefix = bucketPrefix + "channel:"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var voices = []string{
	"Zephyr", "Puck", "Charon", "Kore", "Fenrir", "Leda", "Orus", "Aoede",
	"Callirrhoe", "Autonoe", "Enceladus", "Iapetus", "Umbriel", "Algieba", "Despina",
	"Erinome", "Algenib", "Rasalgethi", "Laomedeia", "Achernar", "Alnilam", "Schedar",
	"Gacrux", "Pulcherrima", "Achird", "Zubenelgenubi", "Vindemiatrix", "Sadachbia", "Sadaltager", "Sulafat",
}

type Config struct {
	BaseURL          string
	APIKey           string
	Model            gossiprpc.TTSModel
	Voice            string
	RateLimit        float64
	ChannelRateLimit float64
}

type synthesizer struct {
	client  *core.HTTPClient
	log     *zap.Logger
	limiter *ratelimit.Limiter
	global  core.Buckets
	channel core.Buckets
	model   gossiprpc.TTSModel
	voice   string
	flights singleflight.Group
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider("tts", d).Trusted()
	log := d.Logger()
	cfg = cfg.withValidDefaults(log)
	api := synthesizer{
		client:  b.Client(strings.TrimRight(cfg.BaseURL, "/"), map[string]string{"x-goog-api-key": cfg.APIKey}, 20*time.Second),
		log:     log,
		limiter: d.Limiter,
		global:  core.NewBuckets(bucketPrefix+"global", cfg.RateLimit, 60),
		channel: core.NewBuckets(channelPrefix, cfg.ChannelRateLimit, 60),
		model:   cfg.Model,
		voice:   cfg.Voice,
	}
	b.Endpoint("voices").Handle(api.listVoices)
	b.Endpoint("synthesize").Timeout(25 * time.Second).Handle(api.synthesize)
	return b.Build()
}

func (cfg Config) withValidDefaults(log *zap.Logger) Config {
	if !slices.Contains(voices, cfg.Voice) {
		log.Warn("invalid GEMINI_TTS_VOICE, using default", zap.String("voice", defaultVoice))
		cfg.Voice = defaultVoice
	}
	if !validModel(cfg.Model) {
		log.Warn("invalid GEMINI_TTS_MODEL, using default", zap.String("model", string(gossiprpc.TTSFlash)))
		cfg.Model = gossiprpc.TTSFlash
	}
	return cfg
}

func validModel(model gossiprpc.TTSModel) bool {
	return model == gossiprpc.TTSFlash || model == gossiprpc.TTSFlashLite
}

func (s *synthesizer) listVoices(context.Context, gossiprpc.Request) any {
	return gossiprpc.TTSVoicesReply{Voices: slices.Clone(voices), Model: s.model}
}

func (s *synthesizer) synthesize(ctx context.Context, req gossiprpc.Request) any {
	id, reject := provider.Channel(req)
	if reject != "" {
		return gossiprpc.TTSSynthesizeReply{Error: reject}
	}
	req.ChannelID = id.Key
	req.Voice = cmp.Or(req.Voice, s.voice)
	req.Model = cmp.Or(req.Model, s.model)
	req.Text = strings.TrimSpace(req.Text)
	req.Style = strings.TrimSpace(req.Style)
	if reject := validate(req); reject != "" {
		return gossiprpc.TTSSynthesizeReply{Error: reject}
	}
	if req.RequestID == "" {
		return s.call(ctx, req)
	}
	reply, _, _ := s.flights.Do(req.ChannelID+":"+req.RequestID, func() (any, error) {
		return s.call(ctx, req), nil
	})
	return reply
}

func (s *synthesizer) call(ctx context.Context, req gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	channel := s.channel.WithKey(channelPrefix + req.ChannelID)
	if err := channel.EnforceGeneralPair(ctx, s.limiter, s.global); err != nil {
		return s.failure(ctx, err)
	}
	return s.fetch(ctx, req)
}

func validate(req gossiprpc.Request) string {
	switch {
	case !validTextLength(req.Text):
		return "invalid text: expected 1 to 500 characters"
	case strings.ContainsFunc(req.Text, forbiddenSpeechRune):
		return "invalid text: control characters are not allowed"
	case !slices.Contains(voices, req.Voice):
		return "unknown voice"
	case utf8.RuneCountInString(req.Style) > 200:
		return "invalid style: expected at most 200 characters"
	case strings.ContainsFunc(req.Style, forbiddenSpeechRune):
		return "invalid style: control characters are not allowed"
	case !validModel(req.Model):
		return "unknown model"
	case !validRequestID(req.RequestID):
		return "invalid request_id: expected up to 64 letters, digits, underscores or hyphens"
	}
	return ""
}

func validTextLength(text string) bool {
	return text != "" && utf8.RuneCountInString(text) <= 500
}

func validRequestID(id string) bool {
	return id == "" || requestIDPattern.MatchString(id)
}

func forbiddenSpeechRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

func (s *synthesizer) failure(ctx context.Context, err error) gossiprpc.TTSSynthesizeReply {
	core.RecordFetchError(ctx, err)
	unavailable := gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}
	var upstream *core.UpstreamError
	if !errors.As(err, &upstream) {
		return unavailable
	}
	switch {
	case upstream.Status == http.StatusTooManyRequests:
		return gossiprpc.TTSSynthesizeReply{
			Error:        "speech synthesis is busy right now, try again shortly",
			RateLimited:  true,
			RetryAfterMS: upstream.RetryAfter.Milliseconds(),
		}
	case errors.Is(err, errKeyRejected):
		monitor.TxnLogger(ctx, s.log).Warn("Gemini TTS API key rejected", zap.Int("status", upstream.Status))
	case upstream.Status >= http.StatusBadRequest && upstream.Status < http.StatusInternalServerError:
		return gossiprpc.TTSSynthesizeReply{Error: "the speech provider refused this request", Reason: "refused"}
	}
	return unavailable
}
