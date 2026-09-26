// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/goveecredential"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/app/db/modules/ent/predicate"
	"ItsBagelBot/app/db/modules/ent/quote"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
)

const accountInstanceKey = "__account_created_at"

var errStaleAccount = errors.New("loyalty module belongs to a different account instance")

type AccountInstanceResolver func(context.Context, uint64) (int64, error)

// SetAccountInstanceResolver must be installed before accepting requests.
func (r *Modules) SetAccountInstanceResolver(resolve AccountInstanceResolver) {
	r.instanceResolver = resolve
}
func accountInstanceMap(cfg map[string]codec.RawMessage) int64 {
	var epoch int64
	_ = codec.Unmarshal(cfg[accountInstanceKey], &epoch)
	if epoch < 0 {
		return 0
	}
	return epoch
}
func accountInstance(blob []byte) int64 { return accountInstanceMap(decodeConfig(blob)) }
func configRevision(blob []byte) int {
	var rev int
	_ = codec.Unmarshal(decodeConfig(blob)[revKey], &rev)
	return rev
}
func (r *Modules) stampLoyalty(ctx context.Context, id uint64, blob []byte) ([]byte, error) {
	cfg := decodeConfig(blob)
	delete(cfg, accountInstanceKey)
	if r.instanceResolver != nil {
		resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		epoch, err := r.instanceResolver(resolveCtx, id)
		if err != nil {
			return nil, fmt.Errorf("resolve loyalty account instance: %w", err)
		}
		if epoch <= 0 {
			return nil, errStaleAccount
		}
		cfg[accountInstanceKey], _ = codec.Marshal(epoch)
	}
	return codec.Marshal(cfg)
}
func (r *Modules) upsertLoyalty(ctx context.Context, item data.ModuleChangedDTO) error {
	epoch := accountInstance(item.Configs)
	if err := r.validateCapturedAccount(ctx, item.UserID, epoch); err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		done, err := r.upsertLoyaltyAttempt(ctx, item, epoch)
		if err != nil || done {
			return err
		}
	}
	return errors.New("loyalty module concurrent update; retry")
}

// DeleteAccount resolves the canonical incarnation before removing modules, quotes
// or credentials. Captured row identities and versions fence concurrent updates;
// rows inserted or replaced after the snapshot are left untouched.
func (r *Modules) DeleteAccount(ctx context.Context, id uint64, epoch int64) error {
	if epoch <= 0 || r.instanceResolver == nil {
		return nil
	}
	snapshot, err := r.captureAccountCleanup(ctx, id)
	if err != nil {
		return err
	}
	allowed, err := r.authorizeAccountCleanup(ctx, id, epoch, snapshot)
	if err != nil || !allowed {
		return err
	}
	applied, err := r.deleteAccountSnapshot(ctx, id, epoch, snapshot)
	if err != nil {
		return err
	}
	if applied {
		r.Invalidate(id)
	}
	return nil
}

func (r *Modules) validateCapturedAccount(ctx context.Context, id uint64, epoch int64) error {
	if r.instanceResolver == nil {
		return nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	current, err := r.instanceResolver(resolveCtx, id)
	if err != nil {
		return err
	}
	if current <= 0 {
		return errStaleAccount
	}
	if current != epoch {
		return errStaleAccount
	}
	return nil
}

func (r *Modules) upsertLoyaltyAttempt(ctx context.Context, item data.ModuleChangedDTO, epoch int64) (bool, error) {
	row, err := r.client.Modules.Query().Where(modules.UserIDEQ(item.UserID), modules.NameEQ("loyalty")).Only(ctx)
	if ent.IsNotFound(err) {
		return r.createLoyaltyAttempt(ctx, item)
	}
	if err != nil {
		return false, err
	}
	if accountInstance(row.Configs) > epoch {
		return false, errStaleAccount
	}
	n, err := r.client.Modules.Update().Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision)).SetIsEnabled(item.IsEnabled).SetConfigs(item.Configs).AddRevision(1).Save(ctx)
	return n == 1, err
}

func (r *Modules) createLoyaltyAttempt(ctx context.Context, item data.ModuleChangedDTO) (bool, error) {
	err := r.client.Modules.Create().SetUserID(item.UserID).SetName("loyalty").SetIsEnabled(item.IsEnabled).SetConfigs(item.Configs).SetRevision(1).Exec(ctx)
	if ent.IsConstraintError(err) {
		return false, nil
	}
	return true, err
}

type accountCleanupSnapshot struct {
	modules []*ent.Modules
	govee   []*ent.GoveeCredential
	spotify []*ent.SpotifyCredential
	quotes  []*ent.Quote
}

func (r *Modules) captureAccountCleanup(ctx context.Context, id uint64) (accountCleanupSnapshot, error) {
	var snapshot accountCleanupSnapshot
	var err error
	snapshot.modules, err = r.client.Modules.Query().Where(modules.UserIDEQ(id)).All(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.govee, err = r.client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(id)).All(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.spotify, err = r.client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(id)).All(ctx)
	if err != nil {
		return snapshot, err
	}
	snapshot.quotes, err = r.client.Quote.Query().Where(quote.UserIDEQ(id)).All(ctx)
	return snapshot, err
}

func (r *Modules) authorizeAccountCleanup(ctx context.Context, id uint64, epoch int64, snapshot accountCleanupSnapshot) (bool, error) {
	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	current, err := r.instanceResolver(resolveCtx, id)
	if err != nil {
		return false, err
	}
	if current < 0 {
		return false, errors.New("invalid canonical account instance")
	}
	if current > 0 && current != epoch {
		return false, nil
	}
	return !snapshot.hasNewerLoyalty(epoch), nil
}

func (s accountCleanupSnapshot) hasNewerLoyalty(epoch int64) bool {
	for _, row := range s.modules {
		if newerLoyaltyRow(row, epoch) {
			return true
		}
	}
	return false
}

func (r *Modules) deleteAccountSnapshot(ctx context.Context, id uint64, epoch int64, snapshot accountCleanupSnapshot) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// A stamped replacement aborts every captured section, not just loyalty.
	newer, err := newerPersistedLoyalty(ctx, tx, id, epoch)
	if err != nil || newer {
		return false, err
	}
	if err := snapshot.deleteCapturedRows(ctx, tx); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func newerPersistedLoyalty(ctx context.Context, tx *ent.Tx, id uint64, epoch int64) (bool, error) {
	row, err := tx.Modules.Query().Where(modules.UserIDEQ(id), modules.NameEQ("loyalty")).Only(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return accountInstance(row.Configs) > epoch, nil
}

func (s accountCleanupSnapshot) deleteCapturedRows(ctx context.Context, tx *ent.Tx) error {
	if err := s.deleteModules(ctx, tx); err != nil {
		return err
	}
	if err := s.deleteGovee(ctx, tx); err != nil {
		return err
	}
	if err := s.deleteSpotify(ctx, tx); err != nil {
		return err
	}
	return s.deleteQuotes(ctx, tx)
}

func (s accountCleanupSnapshot) deleteModules(ctx context.Context, tx *ent.Tx) error {
	for _, row := range s.modules {
		if _, err := tx.Modules.Delete().Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision), modules.UpdatedAtEQ(row.UpdatedAt)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s accountCleanupSnapshot) deleteGovee(ctx context.Context, tx *ent.Tx) error {
	for _, row := range s.govee {
		if _, err := tx.GoveeCredential.Delete().Where(goveecredential.IDEQ(row.ID), goveecredential.UpdatedAtEQ(row.UpdatedAt), goveecredential.KeyEncEQ(row.KeyEnc)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s accountCleanupSnapshot) deleteSpotify(ctx context.Context, tx *ent.Tx) error {
	for _, row := range s.spotify {
		if _, err := tx.SpotifyCredential.Delete().Where(spotifyCleanupConditions(row)...).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func spotifyCleanupConditions(row *ent.SpotifyCredential) []predicate.SpotifyCredential {
	conditions := []predicate.SpotifyCredential{spotifycredential.IDEQ(row.ID), spotifycredential.UpdatedAtEQ(row.UpdatedAt), spotifycredential.ClientIDEQ(row.ClientID), spotifycredential.ScopesEQ(row.Scopes)}
	if row.TokenEnc == nil {
		conditions = append(conditions, spotifycredential.TokenEncIsNil())
	} else {
		conditions = append(conditions, spotifycredential.TokenEncEQ(row.TokenEnc))
	}
	if row.ClientSecretEnc == nil {
		conditions = append(conditions, spotifycredential.ClientSecretEncIsNil())
	} else {
		conditions = append(conditions, spotifycredential.ClientSecretEncEQ(row.ClientSecretEnc))
	}
	return conditions
}

func (s accountCleanupSnapshot) deleteQuotes(ctx context.Context, tx *ent.Tx) error {
	for _, row := range s.quotes {
		if _, err := tx.Quote.Delete().Where(quote.IDEQ(row.ID), quote.CreatedAtEQ(row.CreatedAt), quote.TextEQ(row.Text), quote.AddedByEQ(row.AddedBy)).Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}

func newerLoyaltyRow(row *ent.Modules, epoch int64) bool {
	if row.Name != "loyalty" {
		return false
	}
	return accountInstance(row.Configs) > epoch
}
