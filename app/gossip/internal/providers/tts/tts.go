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
	speechTimeout = 20 * time.Second
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var (
	errNoAudio      = errors.New("speech provider returned no audio")
	errInvalidAudio = errors.New("speech provider returned invalid audio")
	errRefused      = errors.New("the speech provider refused this request")
	errUnavailable  = errors.New("speech synthesis is unavailable right now")
	errInternal     = errors.New("internal error")
)

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
	Vertex           VertexConfig
}

type backend interface {
	generate(context.Context, gossiprpc.Request) (speech, error)
}

type speech struct {
	wav        []byte
	durationMS int64
	usage      tokenUsage
}

type tokenUsage struct {
	audio int64
	total int64
}

type synthesizer struct {
	backend backend
	log     *zap.Logger
	limiter *ratelimit.Limiter
	global  core.Buckets
	channel core.Buckets
	model   gossiprpc.TTSModel
	voice   string
	flights singleflight.Group
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider("tts", d)
	log := d.Logger()
	cfg = cfg.withValidDefaults(log)
	api := synthesizer{
		backend: cfg.backend(b, log),
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

func (cfg Config) backend(b *provider.Builder, log *zap.Logger) backend {
	if cfg.Vertex.Credentials == "" {
		client := b.Trusted().Client(strings.TrimRight(cfg.BaseURL, "/"), map[string]string{"x-goog-api-key": cfg.APIKey}, speechTimeout)
		return geminiBackend{client: client}
	}
	vertex, err := newVertexBackend(b, cfg.Vertex)
	if err != nil {
		log.Warn("invalid Vertex TTS configuration", zap.Error(err))
		return nil
	}
	return vertex
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
	if s.backend == nil {
		return s.failure(ctx, errUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, speechTimeout)
	defer cancel()
	sample, err := s.backend.generate(ctx, req)
	if err != nil {
		return s.failure(ctx, err)
	}
	return sample.reply(req)
}

func (s speech) reply(req gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	return gossiprpc.TTSSynthesizeReply{
		Audio:            s.wav,
		MimeType:         wavMIME,
		DurationMS:       s.durationMS,
		BilledCharacters: utf8.RuneCountInString(req.Text),
		StyleCharacters:  utf8.RuneCountInString(req.Style),
		AudioTokens:      s.usage.audio,
		TotalTokens:      s.usage.total,
		Model:            req.Model,
	}
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
	var upstream *core.UpstreamError
	if errors.As(err, &upstream) {
		return s.upstreamFailure(ctx, upstream, err)
	}
	switch err {
	case errRefused:
		return refusedReply()
	case errNoAudio, errInvalidAudio, errInternal:
		return gossiprpc.TTSSynthesizeReply{Error: err.Error()}
	}
	return gossiprpc.TTSSynthesizeReply{Error: errUnavailable.Error()}
}

func (s *synthesizer) upstreamFailure(ctx context.Context, upstream *core.UpstreamError, err error) gossiprpc.TTSSynthesizeReply {
	switch {
	case upstream.Status == http.StatusTooManyRequests:
		return gossiprpc.TTSSynthesizeReply{
			Error:        "speech synthesis is busy right now, try again shortly",
			RateLimited:  true,
			RetryAfterMS: upstream.RetryAfter.Milliseconds(),
		}
	case errors.Is(err, errKeyRejected):
		monitor.TxnLogger(ctx, s.log).Warn("Gemini TTS API key rejected", zap.Int("status", upstream.Status))
	case errors.Is(err, errVertexCredentialsRejected):
		monitor.TxnLogger(ctx, s.log).Warn("Vertex TTS credentials rejected", zap.Int("status", upstream.Status))
	case upstream.Status >= http.StatusBadRequest && upstream.Status < http.StatusInternalServerError:
		return refusedReply()
	}
	return gossiprpc.TTSSynthesizeReply{Error: errUnavailable.Error()}
}

func refusedReply() gossiprpc.TTSSynthesizeReply {
	return gossiprpc.TTSSynthesizeReply{Error: errRefused.Error(), Reason: "refused"}
}
