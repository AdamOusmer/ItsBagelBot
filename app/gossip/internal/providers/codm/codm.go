// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package codm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	providerName      = "codm"
	defaultBase       = "https://order-sg.codashop.com"
	defaultCountry    = "IN"
	defaultRateLimit  = 60.0
	rateWindowSeconds = 60.0
	cooldownKey       = "gossip:codm:cooldown"

	profileTTL     = 5 * time.Minute
	negativeTTL    = time.Minute
	httpTimeout    = 10 * time.Second
	handlerTimeout = 15 * time.Second

	maxAccountRunes = 64
	maxCountryRunes = 32
	maxRedirects    = 3
)

type Config struct {
	BaseURL   string
	Country   string
	RateLimit float64
}

type api struct {
	http     *core.HTTPClient
	cache    *core.Cache
	log      *zap.Logger
	limiter  *ratelimit.Limiter
	buckets  core.Buckets
	requests core.Buckets
	country  string
	deviceID string
}

func New(cfg Config, d provider.Deps) provider.Provider {
	b := provider.NewProvider(providerName, d)
	p := newAPI(cfg, d, b)
	b.Endpoint("profile").Timeout(handlerTimeout).
		Cached(profileTTL, negativeTTL).
		ID(profileID).
		Reply(func(id, msg string) any {
			return gossiprpc.CODMProfileReply{Player: id, Error: msg}
		}).
		Fallback("profile lookup failed").
		Budget(p.admit).
		Fetch(func(ctx context.Context, req gossiprpc.Request, id provider.ID) (any, error) {
			return p.fetchProfile(ctx, req, id)
		})
	return b.Build()
}

func newAPI(cfg Config, d provider.Deps, b *provider.Builder) *api {
	base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultBase
	}
	country, err := normalizeCountry(cfg.Country)
	if err != nil {
		country = defaultCountry
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultRateLimit
	}
	return &api{
		http:     b.Client(base, nil, httpTimeout),
		cache:    d.Cache,
		log:      d.Logger(),
		limiter:  d.Limiter,
		buckets:  core.NewPacedBuckets("ratelimit:gossip:codm:lookups", cfg.RateLimit, rateWindowSeconds, maxRedirects+1),
		requests: core.NewPacedBuckets("ratelimit:gossip:codm:http", cfg.RateLimit, rateWindowSeconds, maxRedirects+1),
		country:  country,
		deviceID: uuid.NewString(),
	}
}

type validateRequest struct {
	Country         string `json:"country"`
	VoucherTypeName string `json:"voucherTypeName"`
	WhiteLabelID    string `json:"whiteLabelId"`
	DeviceID        string `json:"deviceId"`
	UserID          string `json:"userId"`
}

type validateResponse struct {
	Success              *bool          `json:"success"`
	ErrorCode            int            `json:"errorCode"`
	ErrorMsg             string         `json:"errorMsg"`
	HomeBaseCountry2Name string         `json:"homeBaseCountry2Name"`
	Result               *profileResult `json:"result"`
}

type profileResult struct {
	Type                 string `json:"type"`
	Result               int    `json:"result"`
	Nickname             string `json:"nickname"`
	Level                int    `json:"level"`
	CustomReadableMPRank string `json:"customReadableMpRank"`
	RankClass            int    `json:"rankClass"`
	Rating               int    `json:"rating"`
	CountryID            int    `json:"countryId"`
	ShortID              string `json:"shortId"`
}

func profileID(req gossiprpc.Request) (provider.ID, string) {
	account := strings.TrimSpace(req.Account)
	if err := validateText(account, maxAccountRunes); err != nil {
		return provider.ID{Display: account}, "invalid account"
	}
	return provider.ID{Display: account, Key: account}, ""
}

func validateText(s string, maxRunes int) error {
	if s == "" {
		return errors.New("invalid text")
	}
	if !utf8.ValidString(s) {
		return errors.New("invalid text")
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return errors.New("invalid text")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return errors.New("control character")
		}
	}
	return nil
}

func normalizeCountry(country string) (string, error) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if err := validateText(country, maxCountryRunes); err != nil {
		return "", err
	}
	return country, nil
}

func (p *api) admit(ctx context.Context, req gossiprpc.Request) error {
	if err := p.checkCooldown(ctx); err != nil {
		return err
	}
	return p.buckets.Enforce(ctx, p.limiter, req.IsPremium)
}

func (p *api) checkCooldown(ctx context.Context) error {
	if p.cache == nil {
		return nil
	}
	blocked, err := p.cache.Exists(ctx, cooldownKey)
	if err != nil {
		return err
	}
	if blocked {
		return &core.UpstreamError{Status: http.StatusTooManyRequests, Message: "CODM validation cooldown"}
	}
	return nil
}

func (p *api) spendValidation(ctx context.Context) error {
	if err := p.checkCooldown(ctx); err != nil {
		return err
	}
	err := p.requests.Enforce(ctx, p.limiter, true)
	var denial *core.UpstreamError
	if errors.As(err, &denial) {
		denial.Message = "validation rate limit exceeded"
	}
	return err
}

func (p *api) recordThrottle(ctx context.Context, err error) {
	var throttle *core.UpstreamError
	if !errors.As(err, &throttle) {
		return
	}
	if throttle.Status != http.StatusTooManyRequests {
		return
	}
	if throttle.LocalDeny {
		return
	}
	if p.cache == nil {
		return
	}
	delay := throttle.RetryAfter
	if delay <= 0 {
		delay = negativeTTL
	}
	if err := p.cache.SetJSON(ctx, cooldownKey, true, delay); err != nil {
		p.log.Warn("codm cooldown write failed", zap.Error(err))
	}
}

func (p *api) fetchProfile(ctx context.Context, _ gossiprpc.Request, id provider.ID) (gossiprpc.CODMProfileReply, error) {
	country := p.country
	visited := map[string]struct{}{country: {}}

	for hop := 0; ; hop++ {
		response, err := p.validateAccount(ctx, country, id.Display)
		if err != nil {
			return gossiprpc.CODMProfileReply{}, err
		}
		if response.ErrorCode != -200 {
			return p.profileReply(response, country, id.Display)
		}
		next, err := normalizeCountry(response.HomeBaseCountry2Name)
		if err != nil {
			return gossiprpc.CODMProfileReply{}, fmt.Errorf("invalid codm country redirect: %w", err)
		}
		if _, seen := visited[next]; seen {
			return gossiprpc.CODMProfileReply{}, errors.New("codm country redirect loop")
		}
		if hop >= maxRedirects {
			return gossiprpc.CODMProfileReply{}, errors.New("codm country redirect limit exceeded")
		}
		visited[next] = struct{}{}
		country = next
	}
}

func (p *api) validateAccount(ctx context.Context, country, account string) (validateResponse, error) {
	payload, err := codec.Marshal(validateRequest{
		Country: country, VoucherTypeName: "CALL_OF_DUTY_MOBILE_WL",
		WhiteLabelID: "1", DeviceID: p.deviceID, UserID: account,
	})
	if err != nil {
		return validateResponse{}, fmt.Errorf("encode codm validation request: %w", err)
	}
	if err := p.spendValidation(ctx); err != nil {
		return validateResponse{}, err
	}
	var response validateResponse
	err = p.http.Do(ctx, core.Request{Method: http.MethodPost, Path: "/validate", Body: payload, NoRedirects: true}, &response)
	p.recordThrottle(ctx, err)
	return response, err
}

func (p *api) profileReply(response validateResponse, country, player string) (gossiprpc.CODMProfileReply, error) {
	if response.Success != nil && !*response.Success {
		return gossiprpc.CODMProfileReply{}, &core.UpstreamError{
			Status:  404,
			Message: "player not found",
		}
	}
	if response.Result == nil {
		return gossiprpc.CODMProfileReply{}, errors.New("codm response missing profile")
	}
	result := response.Result
	if result.Type != "SUCCESS" || result.Result != 0 {
		return gossiprpc.CODMProfileReply{}, errors.New("codm response profile is not successful")
	}
	if err := validateProfile(*result); err != nil {
		return gossiprpc.CODMProfileReply{}, err
	}
	return gossiprpc.CODMProfileReply{
		// The upstream nickname is streamer-mode data and must never cross the gossip boundary.
		Player:    player,
		Level:     result.Level,
		Rank:      result.CustomReadableMPRank,
		RankClass: result.RankClass,
		Rating:    result.Rating,
		Country:   country,
		ShortID:   result.ShortID,
	}, nil
}

func validateProfile(result profileResult) error {
	if err := validateText(result.Nickname, maxAccountRunes); err != nil {
		return fmt.Errorf("codm response nickname is malformed: %w", err)
	}
	if err := validateText(result.CustomReadableMPRank, maxCountryRunes); err != nil {
		return fmt.Errorf("codm response rank is malformed: %w", err)
	}
	for _, field := range []struct{ value, minimum int }{
		{result.Level, 1}, {result.RankClass, 0}, {result.Rating, 0}, {result.CountryID, 1},
	} {
		if field.value < field.minimum {
			return errors.New("codm response profile is malformed")
		}
	}
	if result.ShortID != "" && validateText(result.ShortID, 64) != nil {
		return errors.New("codm response short id is malformed")
	}
	return nil
}
