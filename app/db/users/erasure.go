// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/objectstore"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const twitchRevokeURL = "https://id.twitch.tv/oauth2/revoke"

func wireErasure(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	store, err := objectstore.New(objectstore.ConfigFromEnv("ERASURE_R2_"))
	svcboot.FatalIf(log, err, "failed to configure erasure ledger")
	repo.SetErasureStore(store)
	repo.SetTokenRevoker(twitchRevoker{clientID: env.MustGet("TWITCH_CLIENT_ID"), http: monitor.HTTPClient(10 * time.Second)})
	go replayErasures(ctx, repo, log)
}

func replayErasures(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	spec := sweepSpec{
		interval:       env.GetDuration("USERS_ERASURE_REPLAY_INTERVAL", 15*time.Minute),
		timeout:        2 * time.Minute,
		errorMessage:   "failed to replay erasures",
		successMessage: "replayed erasures",
		run:            repo.ReplayErasures,
	}
	spec.runOnce(ctx, log, time.Now())
	runPeriodicSweep(ctx, log, spec)
}

type twitchRevoker struct {
	clientID string
	http     *http.Client
}

func (t twitchRevoker) Revoke(ctx context.Context, accessToken string) error {
	form := url.Values{"client_id": {t.clientID}, "token": {accessToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twitchRevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	// 400 is Twitch's "Invalid token": already expired or revoked.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest {
		return nil
	}
	return fmt.Errorf("twitch revoke: status %d", resp.StatusCode)
}
