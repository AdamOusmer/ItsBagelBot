// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

const (
	defaultVoice  = voiceplan.DefaultVoice
	speechTimeout = 150 * time.Second
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var (
	errNoAudio      = errors.New("speech provider returned no audio")
	errInvalidAudio = errors.New("speech provider returned invalid audio")
	errRefused      = errors.New("the speech provider refused this request")
	errUnavailable  = errors.New("speech synthesis is unavailable right now")
	errInternal     = errors.New("internal error")
)

type Config struct {
	BaseURL     string
	APIKey      string
	Model       gossiprpc.TTSModel
	Voice       string
	Temperature float64
	Vertex      VertexConfig
	VertexOnly  bool
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
	model   gossiprpc.TTSModel
	voice   string
	flights singleflight.Group
}

type Synthesizer struct {
	api *synthesizer
}

func NewSynthesizer(cfg Config, d provider.Deps) *Synthesizer {
	_, api := build(cfg, d)
	return &Synthesizer{api: api}
}

func (s *Synthesizer) Synthesize(ctx context.Context, req gossiprpc.Request) gossiprpc.TTSSynthesizeReply {
	reply, _ := s.api.synthesize(ctx, req).(gossiprpc.TTSSynthesizeReply)
	return reply
}

func build(cfg Config, d provider.Deps) (*provider.Builder, *synthesizer) {
	b := provider.NewProvider("tts", d)
	log := d.Logger()
	cfg = cfg.withValidDefaults(log)
	return b, &synthesizer{
		backend: cfg.backend(b, log),
		log:     log,
		model:   cfg.Model,
		voice:   cfg.Voice,
	}
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b, api := build(cfg, d)
	b.Endpoint("voices").Handle(api.listVoices)
	b.Endpoint("synthesize").Timeout(25 * time.Second).Handle(api.synthesize)
	return b.Build()
}

func (cfg Config) backend(b *provider.Builder, log *zap.Logger) backend {
	if cfg.Vertex.Credentials == "" {
		if cfg.VertexOnly {
			return nil
		}
		client := b.Trusted().Client(strings.TrimRight(cfg.BaseURL, "/"), map[string]string{"x-goog-api-key": cfg.APIKey}, speechTimeout)
		return geminiBackend{client: client, temperature: cfg.Temperature}
	}
	vertex, err := newVertexBackend(b, cfg.Vertex, cfg.Temperature)
	if err != nil {
		log.Warn("invalid Vertex TTS configuration", zap.Error(err))
		return nil
	}
	return vertex
}

func (cfg Config) withValidDefaults(log *zap.Logger) Config {
	if !voiceplan.ValidVoice(cfg.Voice) {
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
	return gossiprpc.TTSVoicesReply{Voices: voiceplan.VoiceNames(), Model: s.model}
}

func (s *synthesizer) synthesize(ctx context.Context, req gossiprpc.Request) any {
	id, reject := provider.Channel(req)
	if reject != "" {
		return gossiprpc.TTSSynthesizeReply{Error: reject}
	}
	req.ChannelID = id.Key
	req.Voice = cmp.Or(req.Voice, s.voice)
	req.Model = cmp.Or(req.Model, s.model)
	req.Text = Shape(req.Text)
	req.Style = strings.TrimSpace(req.Style)
	if reply, refused := localRefusal(ctx, req.Text); refused {
		return reply
	}
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
	if s.backend == nil {
		return s.failure(ctx, errUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, speechTimeout)
	defer cancel()
	sample, err := s.backend.generate(ctx, req)
	if err != nil {
		return s.failure(ctx, err)
	}
	reply := sample.reply(req)
	recordSpeech(ctx, reply)
	return reply
}

func recordSpeech(ctx context.Context, reply gossiprpc.TTSSynthesizeReply) {
	txn := newrelic.FromContext(ctx)
	txn.AddAttribute("tts.model", string(reply.Model))
	txn.AddAttribute("tts.duration_ms", reply.DurationMS)
	txn.AddAttribute("tts.audio_tokens", reply.AudioTokens)
	txn.AddAttribute("tts.characters", reply.BilledCharacters)
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
		return fmt.Sprintf("invalid text: expected 1 to %d characters", voiceplan.MaxSpeechRunes)
	case strings.ContainsFunc(req.Text, forbiddenSpeechRune):
		return "invalid text: control characters are not allowed"
	case !voiceplan.ValidVoice(req.Voice):
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
	return text != "" && utf8.RuneCountInString(text) <= voiceplan.MaxSpeechRunes
}

func validRequestID(id string) bool {
	return id == "" || requestIDPattern.MatchString(id)
}

func forbiddenSpeechRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

func (s *synthesizer) failure(ctx context.Context, err error) gossiprpc.TTSSynthesizeReply {
	core.RecordFetchError(ctx, err)
	reply := s.failureReply(ctx, err)
	monitor.TxnLogger(ctx, s.log).Warn("TTS synthesis failed", zap.String("reason", cmp.Or(reply.Reason, reply.Error)),
		zap.Int("status", upstreamStatus(err)), zap.Error(monitor.ScrubError(err)))
	return reply
}

func upstreamStatus(err error) int {
	var upstream *core.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Status
	}
	return 0
}

func (s *synthesizer) failureReply(ctx context.Context, err error) gossiprpc.TTSSynthesizeReply {
	if reply, refused := s.refusalReply(ctx, err); refused {
		return reply
	}
	var upstream *core.UpstreamError
	if errors.As(err, &upstream) {
		return s.upstreamFailure(ctx, upstream, err)
	}
	switch err {
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
		return refusal{class: gossiprpc.RefusalInvalid}.reply()
	}
	return gossiprpc.TTSSynthesizeReply{Error: errUnavailable.Error()}
}
