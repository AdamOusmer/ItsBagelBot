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
	"ItsBagelBot/pkg/objstore"
	"ItsBagelBot/pkg/svcboot"

	"go.uber.org/zap"
)

const twitchRevokeURL = "https://id.twitch.tv/oauth2/revoke"

func wireErasure(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	store, err := objstore.New(objstore.Config{
		Endpoint:        env.MustGet("ERASURE_R2_ENDPOINT"),
		Bucket:          env.MustGet("ERASURE_R2_BUCKET"),
		AccessKeyID:     env.MustGet("ERASURE_R2_ACCESS_KEY_ID"),
		SecretAccessKey: env.MustGet("ERASURE_R2_SECRET_ACCESS_KEY"),
	})
	svcboot.FatalIf(log, err, "failed to configure erasure ledger")
	repo.SetErasureStore(store)
	repo.SetTokenRevoker(twitchRevoker{clientID: env.MustGet("TWITCH_CLIENT_ID"), http: &http.Client{Timeout: 10 * time.Second}})
	go replayErasures(ctx, repo, log)
}

func replayErasures(ctx context.Context, repo *repository.Users, log *zap.Logger) {
	const timeout = 2 * time.Minute
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	count, err := repo.ReplayErasures(runCtx, time.Now())
	cancel()
	logReplay(log, count, err)
	runPeriodicSweep(ctx, log, env.GetDuration("USERS_ERASURE_REPLAY_INTERVAL", 15*time.Minute), timeout,
		"failed to replay erasures", "replayed erasures", repo.ReplayErasures)
}

func logReplay(log *zap.Logger, count int, err error) {
	if err != nil {
		log.Error("failed to replay erasures", zap.Error(err))
	} else if count > 0 {
		log.Info("replayed erasures", zap.Int("count", count))
	}
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
