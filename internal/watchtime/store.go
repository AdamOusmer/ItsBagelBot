// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package watchtime owns primary-consistent admission and the replayable watch
// award outbox. Accepted work must survive downstream SQL failures. Deployment
// persistence and replication determine the outbox's infrastructure durability.
package watchtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	pkgvalkey "ItsBagelBot/pkg/valkey"
	"github.com/valkey-io/valkey-go"
)

const Stream = "watchtime:outbox"

func AdmissionKey(id uint64) string { return "watchtime:admission:" + strconv.FormatUint(id, 10) }

type Snapshot struct {
	BroadcasterID    uint64
	Generation       string
	LiveSession      string
	AccountCreatedAt int64
	Config           codec.RawMessage
}
type Store struct{ client valkey.Client }

func NewStore(client valkey.Client) *Store { return &Store{client: pkgvalkey.Primary(client)} }

func (s *Store) Capture(ctx context.Context, id uint64) (Snapshot, bool, error) {
	if id == 0 {
		return Snapshot{}, false, nil
	}
	sid := strconv.FormatUint(id, 10)
	vals, err := pkgvalkey.RunOutboxAdmissionCapture(ctx, s.client, []string{"settings:" + sid, AdmissionKey(id), "live:" + sid, "trial:desired", "loyaltick:state:" + sid}, []string{sid}).AsStrSlice()
	if valkey.IsValkeyNil(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(vals) != 4 {
		return Snapshot{}, false, errors.New("invalid watch admission reply")
	}
	instance, err := strconv.ParseInt(vals[2], 10, 64)
	if err != nil {
		return Snapshot{}, false, err
	}
	return Snapshot{id, vals[0], vals[1], instance, codec.RawMessage(vals[3])}, true, nil
}
func (s *Store) Validate(ctx context.Context, snapshot Snapshot) (bool, error) {
	current, ok, err := s.Capture(ctx, snapshot.BroadcasterID)
	return ok && current.Generation == snapshot.Generation && current.LiveSession == snapshot.LiveSession && current.AccountCreatedAt == snapshot.AccountCreatedAt, err
}
func OperationID(a data.WatchAwardDTO) string {
	sum := sha256.Sum256([]byte(strconv.FormatUint(a.UserID, 10) + ":" + strconv.FormatInt(a.AccountCreatedAt, 10) + ":" + a.Generation + ":" + a.LiveSession + ":" + a.WindowID + ":" + strconv.FormatUint(uint64(a.Chunk), 10)))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Enqueue(ctx context.Context, a data.WatchAwardDTO) (bool, error) {
	return s.EnqueueOwned(ctx, a, "")
}
func (s *Store) EnqueueOwned(ctx context.Context, a data.WatchAwardDTO, owner string) (bool, error) {
	if err := ValidateAward(a); err != nil {
		return false, err
	}
	body, err := codec.Marshal(a)
	if err != nil {
		return false, err
	}
	digest := sha256.Sum256(body)
	sid := strconv.FormatUint(a.UserID, 10)
	keys := []string{"settings:" + sid, AdmissionKey(a.UserID), "live:" + sid, "trial:desired", Stream, "watchtime:operations:" + sid, "loyaltick:claim:" + sid, "loyaltick:state:" + sid, RetentionFloor, OutboxWindowIndex}
	args := []string{sid, a.Generation, a.LiveSession, OperationID(a), string(body), hex.EncodeToString(digest[:]), owner, a.WindowID, strconv.FormatInt(a.AccountCreatedAt, 10), strconv.FormatInt(WindowStartUnixMilli(a), 10)}
	n, err := pkgvalkey.RunOutboxAdmissionEnqueue(ctx, s.client, keys, args).AsInt64()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return n == 1, err
}

// RestoreAccount accepts only the same still-active instance or a strictly
// newer creation timestamp. Delayed UserChanged cannot revive a deleted instance.
func (s *Store) RestoreAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	if id == 0 || createdAt <= 0 {
		return false, errors.New("invalid account instance")
	}
	keys := []string{AdmissionKey(id), "settings:" + strconv.FormatUint(id, 10), "loyaltick:state:" + strconv.FormatUint(id, 10), "loyaltick:claim:" + strconv.FormatUint(id, 10)}
	n, err := pkgvalkey.RunInstanceFenceRestore(ctx, s.client, keys, []string{strconv.FormatInt(createdAt, 10)}).AsInt64()
	return n == 1, err
}

// DeleteAccount retires only its instance, atomically revoking outstanding
// work and removing settings/scheduler state. A delayed old deletion is ignored.
func (s *Store) DeleteAccount(ctx context.Context, id uint64, createdAt int64) (bool, error) {
	if id == 0 || createdAt <= 0 {
		return false, errors.New("invalid account instance")
	}
	sid := strconv.FormatUint(id, 10)
	keys := []string{AdmissionKey(id), "settings:" + sid, "loyaltick:state:" + sid, "loyaltick:claim:" + sid, "loyaltick:" + sid, "loyaltick:due", "live:" + sid}
	n, err := pkgvalkey.RunInstanceFenceDelete(ctx, s.client, keys, []string{strconv.FormatInt(createdAt, 10), sid}).AsInt64()
	return n == 1, err
}
