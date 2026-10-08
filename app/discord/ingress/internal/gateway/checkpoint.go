// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	checkpointEvery = 5 * time.Second
	checkpointSeqs  = 100
	checkpointSave  = 2 * time.Second
)

type Resume struct {
	SessionID string `json:"session_id"`
	ResumeURL string `json:"resume_url"`
	Seq       int    `json:"seq"`
}

type Checkpoint interface {
	Load(ctx context.Context) (Resume, bool, error)
	Save(ctx context.Context, r Resume) error
}

type checkpointStore struct {
	client valkey.Client
	kv     pkg_valkey.KV
}

func NewCheckpointStore(client valkey.Client) Checkpoint {
	return checkpointStore{client: client, kv: pkg_valkey.NewKV(client)}
}

func (c checkpointStore) Load(ctx context.Context) (Resume, bool, error) {
	raw, err := c.client.Do(ctx, c.client.B().Get().Key(ddiscord.BotCheckpointKey).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return Resume{}, false, nil
	}
	if err != nil {
		return Resume{}, false, err
	}
	var r Resume
	if err := codec.Unmarshal([]byte(raw), &r); err != nil {
		return Resume{}, false, err
	}
	return r, true, nil
}

func (c checkpointStore) Save(ctx context.Context, r Resume) error {
	return pkg_valkey.SetJSON(ctx, c.kv, pkg_valkey.Key{Name: ddiscord.BotCheckpointKey, TTL: ddiscord.BotCheckpointTTL}, r)
}

func (r *resumeState) restore(c Resume) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = c.SessionID
	r.resumeURL = c.ResumeURL
	r.lastID = c.SessionID
	r.savedSeq = c.Seq
	r.seq = nil
	if c.Seq > 0 {
		v := c.Seq
		r.seq = &v
	}
}

func (r *resumeState) snapshot() (Resume, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *resumeState) snapshotLocked() (Resume, bool) {
	if r.sessionID == "" {
		return Resume{}, false
	}
	out := Resume{SessionID: r.sessionID, ResumeURL: r.resumeURL}
	if r.seq != nil {
		out.Seq = *r.seq
	}
	return out, true
}

func (r *resumeState) dueCheckpoint(now time.Time) (Resume, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.snapshotLocked()
	if !ok || cur.Seq == r.savedSeq {
		return Resume{}, false
	}
	if cur.Seq-r.savedSeq < checkpointSeqs && now.Sub(r.savedAt) < checkpointEvery {
		return Resume{}, false
	}
	r.markSavedLocked(cur, now)
	return cur, true
}

func (r *resumeState) markSaved(c Resume, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markSavedLocked(c, now)
}

func (r *resumeState) markSavedLocked(c Resume, now time.Time) {
	r.savedSeq = c.Seq
	r.savedAt = now
}

func (s Session) loadCheckpoint(ctx context.Context, st *resumeState) {
	if s.Checkpoint == nil {
		return
	}
	c, ok, err := s.Checkpoint.Load(ctx)
	if err != nil {
		s.log().Warn("discord gateway checkpoint load failed; identifying", zap.Error(err))
		return
	}
	if !ok || c.SessionID == "" {
		return
	}
	st.restore(c)
	s.log().Info("discord gateway restored checkpoint",
		zap.String("session_id", c.SessionID), zap.Int("seq", c.Seq))
}

func (s Session) saveCheckpoint(ctx context.Context, st *resumeState) {
	if s.Checkpoint == nil {
		return
	}
	cur, ok := st.snapshot()
	if !ok {
		return
	}
	st.markSaved(cur, time.Now())
	s.writeCheckpoint(ctx, cur)
}

func (s Session) checkpointIfDue(ctx context.Context, st *resumeState) {
	if s.Checkpoint == nil {
		return
	}
	if cur, due := st.dueCheckpoint(time.Now()); due {
		s.writeCheckpoint(ctx, cur)
	}
}

func (s Session) writeCheckpoint(ctx context.Context, cur Resume) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), checkpointSave)
	defer cancel()
	if err := s.Checkpoint.Save(wctx, cur); err != nil {
		s.log().Warn("discord gateway checkpoint save failed", zap.Error(err))
	}
}
