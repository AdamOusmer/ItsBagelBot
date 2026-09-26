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
	if r.instanceResolver != nil {
		resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		current, err := r.instanceResolver(resolveCtx, item.UserID)
		if err != nil {
			return err
		}
		if current <= 0 || current != epoch {
			return errStaleAccount
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		row, err := r.client.Modules.Query().Where(modules.UserIDEQ(item.UserID), modules.NameEQ("loyalty")).Only(ctx)
		if ent.IsNotFound(err) {
			err = r.client.Modules.Create().SetUserID(item.UserID).SetName("loyalty").SetIsEnabled(item.IsEnabled).SetConfigs(item.Configs).SetRevision(1).Exec(ctx)
			if ent.IsConstraintError(err) {
				continue
			}
			return err
		}
		if err != nil {
			return err
		}
		if accountInstance(row.Configs) > epoch {
			return errStaleAccount
		}
		n, err := r.client.Modules.Update().Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision)).SetIsEnabled(item.IsEnabled).SetConfigs(item.Configs).AddRevision(1).Save(ctx)
		if err != nil {
			return err
		}
		if n == 1 {
			return nil
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
	rows, err := r.client.Modules.Query().Where(modules.UserIDEQ(id)).All(ctx)
	if err != nil {
		return err
	}
	govee, err := r.client.GoveeCredential.Query().Where(goveecredential.UserIDEQ(id)).All(ctx)
	if err != nil {
		return err
	}
	spotify, err := r.client.SpotifyCredential.Query().Where(spotifycredential.UserIDEQ(id)).All(ctx)
	if err != nil {
		return err
	}
	quotes, err := r.client.Quote.Query().Where(quote.UserIDEQ(id)).All(ctx)
	if err != nil {
		return err
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	current, err := r.instanceResolver(resolveCtx, id)
	cancel()
	if err != nil {
		return err
	}
	if current < 0 {
		return errors.New("invalid canonical account instance")
	}
	if current > 0 && current != epoch {
		return nil
	}
	for _, row := range rows {
		if row.Name == "loyalty" && accountInstance(row.Configs) > epoch {
			return nil
		}
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Recheck persisted loyalty ownership using the transaction's current row.
	// A concurrent stamped replacement aborts cleanup of every captured section.
	loyalty, err := tx.Modules.Query().Where(modules.UserIDEQ(id), modules.NameEQ("loyalty")).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return err
	}
	if err == nil && accountInstance(loyalty.Configs) > epoch {
		return nil
	}
	for _, row := range rows {
		if _, err := tx.Modules.Delete().Where(modules.IDEQ(row.ID), modules.RevisionEQ(row.Revision), modules.UpdatedAtEQ(row.UpdatedAt)).Exec(ctx); err != nil {
			return err
		}
	}
	for _, row := range govee {
		if _, err := tx.GoveeCredential.Delete().Where(goveecredential.IDEQ(row.ID), goveecredential.UpdatedAtEQ(row.UpdatedAt), goveecredential.KeyEncEQ(row.KeyEnc)).Exec(ctx); err != nil {
			return err
		}
	}
	for _, row := range spotify {
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
		if _, err := tx.SpotifyCredential.Delete().Where(conditions...).Exec(ctx); err != nil {
			return err
		}
	}
	for _, row := range quotes {
		if _, err := tx.Quote.Delete().Where(quote.IDEQ(row.ID), quote.CreatedAtEQ(row.CreatedAt), quote.TextEQ(row.Text), quote.AddedByEQ(row.AddedBy)).Exec(ctx); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.Invalidate(id)
	return nil
}
