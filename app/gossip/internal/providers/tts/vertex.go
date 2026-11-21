// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/provider"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
)

type VertexConfig struct {
	Credentials string
	Project     string
	Location    string
	routeOrigin func(string) string
}

var (
	vertexLocationPattern        = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	errVertexCredentialsRejected = errors.New("Vertex TTS credentials rejected")
)

type vertexBackend struct {
	client      *core.HTTPClient
	auth        *vertexAuth
	path        string
	temperature float64
}

func newVertexBackend(b *provider.Builder, cfg VertexConfig, temperature float64) (*vertexBackend, error) {
	account, err := parseServiceAccount(cfg.Credentials)
	if err != nil {
		return nil, err
	}
	project := cmp.Or(cfg.Project, account.project)
	if project == "" {
		return nil, errors.New("Vertex project is missing")
	}
	location := cmp.Or(cfg.Location, "global")
	if !vertexLocationPattern.MatchString(location) {
		return nil, errors.New("invalid Vertex location")
	}
	b.Trusted()
	tokenOrigin := account.tokenURI.Scheme + "://" + account.tokenURI.Host
	return &vertexBackend{
		client: b.Client(cfg.clientOrigin(vertexOrigin(location)), nil, speechTimeout),
		auth: &vertexAuth{
			client:  b.Client(cfg.clientOrigin(tokenOrigin), nil, speechTimeout),
			account: account,
		},
		path:        "/v1/projects/" + url.PathEscape(project) + "/locations/" + location + "/publishers/google/models/",
		temperature: temperature,
	}, nil
}

func (cfg VertexConfig) clientOrigin(origin string) string {
	if cfg.routeOrigin != nil {
		return cfg.routeOrigin(origin)
	}
	return origin
}

func vertexOrigin(location string) string {
	switch location {
	case "global":
		return "https://aiplatform.googleapis.com"
	case "us", "eu":
		return "https://aiplatform." + location + ".rep.googleapis.com"
	default:
		return "https://" + location + "-aiplatform.googleapis.com"
	}
}

func (v *vertexBackend) generate(ctx context.Context, req gossiprpc.Request) (speech, error) {
	token, err := v.auth.token(ctx)
	if err != nil {
		return speech{}, err
	}
	body, err := codec.Marshal(v.requestFor(req))
	if err != nil {
		return speech{}, errInternal
	}
	var result vertexResponse
	err = v.client.Do(ctx, core.Request{
		Method:      http.MethodPost,
		Path:        v.path + string(req.Model) + ":generateContent",
		Headers:     map[string]string{"Authorization": "Bearer " + token},
		Body:        body,
		NoRedirects: true,
		DecodeError: decodeVertexError,
	}, &result)
	if err != nil {
		return speech{}, err
	}
	return result.speech()
}

func decodeVertexError(upstream *core.UpstreamError, body []byte) error {
	switch upstream.Status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: %w", errVertexCredentialsRejected, upstream)
	case http.StatusTooManyRequests:
		upstream.RetryAfter = readGoogleError(body).retryAfter(upstream.RetryAfter)
	}
	return upstream
}

type vertexRequest struct {
	Contents         []vertexContent        `json:"contents"`
	GenerationConfig vertexGenerationConfig `json:"generationConfig"`
}

type vertexContent struct {
	Role  string           `json:"role,omitempty"`
	Parts []vertexTextPart `json:"parts"`
}

type vertexTextPart struct {
	Text           string                `json:"text"`
	SpeechMetadata *vertexSpeechMetadata `json:"speechMetadata,omitempty"`
}

type vertexSpeechMetadata struct {
	Style string `json:"style"`
}

type vertexGenerationConfig struct {
	ResponseModalities []string           `json:"responseModalities"`
	SpeechConfig       vertexSpeechConfig `json:"speechConfig"`
	Temperature        float64            `json:"temperature"`
}

type vertexSpeechConfig struct {
	VoiceConfig vertexVoiceConfig `json:"voiceConfig"`
}

type vertexVoiceConfig struct {
	PrebuiltVoiceConfig vertexPrebuiltVoice `json:"prebuiltVoiceConfig"`
}

type vertexPrebuiltVoice struct {
	VoiceName string `json:"voiceName"`
}

func (v *vertexBackend) requestFor(req gossiprpc.Request) vertexRequest {
	part := vertexTextPart{Text: req.Text}
	if req.Style != "" {
		part.SpeechMetadata = &vertexSpeechMetadata{Style: req.Style}
	}
	return vertexRequest{
		Contents: []vertexContent{{Role: "user", Parts: []vertexTextPart{part}}},
		GenerationConfig: vertexGenerationConfig{
			ResponseModalities: []string{"AUDIO"},
			Temperature:        v.temperature,
			SpeechConfig: vertexSpeechConfig{VoiceConfig: vertexVoiceConfig{
				PrebuiltVoiceConfig: vertexPrebuiltVoice{VoiceName: req.Voice},
			}},
		},
	}
}
