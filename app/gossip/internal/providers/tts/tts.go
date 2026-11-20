// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"errors"
	"net/http"
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
)

const bucketPrefix = "ratelimit:gossip:{tts}:"

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
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider("tts", d).Trusted()
	api := synthesizer{
		client:  b.Client(strings.TrimRight(cfg.BaseURL, "/"), map[string]string{"x-goog-api-key": cfg.APIKey}, 20*time.Second),
		log:     d.Logger(),
		limiter: d.Limiter,
		global:  core.NewBuckets(bucketPrefix+"global", cfg.RateLimit, 60),
		channel: core.NewBuckets(bucketPrefix+"channel:", cfg.ChannelRateLimit, 60),
		model:   cfg.Model,
		voice:   cfg.Voice,
	}
	b.Endpoint("voices").Handle(api.listVoices)
	b.Endpoint("synthesize").Timeout(25 * time.Second).Handle(api.synthesize)
	return b.Build()
}

func (s synthesizer) listVoices(context.Context, gossiprpc.Request) any {
	return gossiprpc.TTSVoicesReply{Voices: slices.Clone(voices), Model: s.model}
}

func (s synthesizer) synthesize(ctx context.Context, req gossiprpc.Request) any {
	id, reject := provider.Channel(req)
	if reject != "" {
		return gossiprpc.TTSSynthesizeReply{Error: reject}
	}
	req.Voice = cmp.Or(req.Voice, s.voice)
	req.Model = cmp.Or(req.Model, s.model)
	text := strings.TrimSpace(req.Text)
	billed := utf8.RuneCountInString(text)
	if reject := validate(req, billed); reject != "" {
		return gossiprpc.TTSSynthesizeReply{Error: reject}
	}
	channel := s.channel.WithKey(bucketPrefix + "channel:" + id.Key)
	if err := channel.EnforceGeneralPair(ctx, s.limiter, s.global); err != nil {
		return s.failure(ctx, err)
	}
	req.Text = text
	return s.fetch(ctx, req, billed)
}

func validate(req gossiprpc.Request, billed int) string {
	switch {
	case billed == 0, billed > 500:
		return "invalid text: expected 1 to 500 characters"
	case strings.ContainsFunc(req.Text, unicode.IsControl):
		return "invalid text: control characters are not allowed"
	case !slices.Contains(voices, req.Voice):
		return "unknown voice"
	case utf8.RuneCountInString(req.Style) > 200:
		return "invalid style: expected at most 200 characters"
	case req.Model != gossiprpc.TTSFlash && req.Model != gossiprpc.TTSFlashLite:
		return "unknown model"
	}
	return ""
}

func (s synthesizer) failure(ctx context.Context, err error) gossiprpc.TTSSynthesizeReply {
	core.RecordFetchError(ctx, err)
	unavailable := gossiprpc.TTSSynthesizeReply{Error: "speech synthesis is unavailable right now"}
	var upstream *core.UpstreamError
	if !errors.As(err, &upstream) {
		return unavailable
	}
	switch upstream.Status {
	case http.StatusTooManyRequests:
		return gossiprpc.TTSSynthesizeReply{
			Error:        "speech synthesis is busy right now, try again shortly",
			RateLimited:  true,
			RetryAfterMS: upstream.RetryAfter.Milliseconds(),
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		monitor.TxnLogger(ctx, s.log).Warn("Gemini TTS API key rejected", zap.Int("status", upstream.Status))
	}
	return unavailable
}
