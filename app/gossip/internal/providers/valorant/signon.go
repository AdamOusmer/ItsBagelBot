// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valorant

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"ItsBagelBot/app/gossip/internal/core"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"

	"go.uber.org/zap"
)

type signOnToken struct {
	AccessToken string `json:"access_token"`
}

type signedInAccount struct {
	Puuid    string `json:"puuid"`
	GameName string `json:"gameName"`
	TagLine  string `json:"tagLine"`
}

type activeShard struct {
	ActiveShard string `json:"activeShard"`
}

func orDefaultURL(raw, fallback string) string {
	if trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "/"); trimmed != "" {
		return trimmed
	}
	return fallback
}

func (c SignOnConfig) ready() bool {
	return c.ClientID != "" && c.ClientSecret != ""
}

func (c SignOnConfig) basicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.ClientID+":"+c.ClientSecret))
}

func (p *api) linkExchange(ctx context.Context, req gossiprpc.Request) any {
	if p.riot == nil || !p.riot.signOn.ready() {
		return gossiprpc.ValorantLinkReply{Error: "Riot sign-in is not set up yet"}
	}
	code, redirect := strings.TrimSpace(req.Code), strings.TrimSpace(req.RedirectURI)
	if code == "" || redirect == "" {
		return gossiprpc.ValorantLinkReply{Error: "missing authorization code"}
	}
	reply, err := p.signIn(ctx, code, redirect)
	if err != nil {
		p.log.Warn("riot sign-in exchange failed", zap.String("broadcaster", req.ChannelID), zap.Error(err))
		return gossiprpc.ValorantLinkReply{Error: "Riot sign-in failed, try again"}
	}
	return reply
}

func (p *api) signIn(ctx context.Context, code, redirect string) (gossiprpc.ValorantLinkReply, error) {
	var token signOnToken
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}}
	err := p.riot.auth.Do(ctx, core.Request{
		Method: http.MethodPost,
		Path:   "/token",
		Headers: map[string]string{
			"Content-Type":  "application/x-www-form-urlencoded",
			"Authorization": p.riot.signOn.basicAuth(),
		},
		Body: []byte(form.Encode()),
	}, &token)
	if err != nil {
		return gossiprpc.ValorantLinkReply{}, err
	}
	var account signedInAccount
	err = p.riot.account.Do(ctx, core.Request{
		Method:  http.MethodGet,
		Path:    "/riot/account/v1/accounts/me",
		Headers: map[string]string{"Authorization": "Bearer " + token.AccessToken},
	}, &account)
	if err != nil {
		return gossiprpc.ValorantLinkReply{}, err
	}
	var shard activeShard
	if err := p.riot.account.GetJSON(ctx, "/riot/account/v1/active-shards/by-game/val/by-puuid/"+url.PathEscape(account.Puuid), nil, &shard); err != nil {
		return gossiprpc.ValorantLinkReply{}, err
	}
	return gossiprpc.ValorantLinkReply{
		Puuid: account.Puuid, GameName: account.GameName, TagLine: account.TagLine,
		Shard: strings.ToLower(shard.ActiveShard),
	}, nil
}
