// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package custom

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"
)

const (
	maxPostBodyBytes  = 4096
	postDrainBytes    = 4096
	contentTypeJSON   = "application/json"
	contentTypeText   = "text/plain; charset=utf-8"
	contentTypeHeader = "Content-Type"
)

func (p *api) post(ctx context.Context, req gossiprpc.Request) any {
	start := time.Now()
	fl, status := p.planPost(ctx, req)
	if fl == nil {
		return postReply(status, nil, start)
	}
	if admit := p.admitFor(req, fl); admit != nil {
		if err := admit(ctx); err != nil {
			return postReply(p.classify(err), err, start)
		}
	}
	err := p.sendPost(ctx, fl, req.Body)
	if err != nil {
		return postReply(p.classify(err), err, start)
	}
	return postReply(gossiprpc.FetchOK, nil, start)
}

func (p *api) planPost(ctx context.Context, req gossiprpc.Request) (*flight, gossiprpc.FetchStatus) {
	channelID := strings.TrimSpace(req.ChannelID)
	if badPostDef(channelID, req) {
		return nil, gossiprpc.FetchBadDef
	}
	return p.admitFlight(ctx, &flight{
		channelID: channelID,
		def:       &gossiprpc.FetchDef{Name: "post:" + channelID, URL: req.Def.URL, IsActive: true},
	})
}

func badPostDef(channelID string, req gossiprpc.Request) bool {
	if channelID == "" || req.Def == nil {
		return true
	}
	return req.Def.URL == "" || len(req.Body) > maxPostBodyBytes
}

func (p *api) sendPost(ctx context.Context, fl *flight, body string) error {
	fctx, cancel := context.WithTimeout(ctx, upstreamBudget)
	defer cancel()
	err := p.http.SendDiscard(fctx, core.Request{
		Method:      http.MethodPost,
		Path:        fl.def.URL,
		Headers:     map[string]string{contentTypeHeader: postContentType(body)},
		Body:        []byte(body),
		NoRedirects: true,
	}, postDrainBytes)
	p.noteOutcome(ctx, fl.host, err)
	return err
}

func postContentType(body string) string {
	if codec.Valid([]byte(body)) {
		return contentTypeJSON
	}
	return contentTypeText
}

func postReply(status gossiprpc.FetchStatus, err error, start time.Time) gossiprpc.CustomFetchReply {
	reply := fetchReply(status, nil, start)
	var ue *core.UpstreamError
	if errors.As(err, &ue) && status == gossiprpc.FetchUpstreamError {
		reply.Error = fmt.Sprintf("upstream status %d", ue.Status)
	}
	return reply
}
