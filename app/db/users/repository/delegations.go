// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/delegation"
	"ItsBagelBot/pkg/db"
)

type DelegationView struct {
	Token         string   `json:"token"`
	OwnerID       uint64   `json:"owner_id"`
	OwnerLogin    string   `json:"owner_login"`
	Sections      []string `json:"sections"`
	DelegateID    uint64   `json:"delegate_id"`
	DelegateLogin string   `json:"delegate_login"`
	Consumed      bool     `json:"consumed"`
}

func toDelegationView(d *ent.Delegation) DelegationView {
	return DelegationView{
		Token:         d.Token,
		OwnerID:       d.OwnerID,
		OwnerLogin:    d.OwnerLogin,
		Sections:      d.Sections,
		DelegateID:    d.DelegateID,
		DelegateLogin: d.DelegateLogin,
		Consumed:      d.ConsumedAt != nil,
	}
}

func (r *Users) CreateDelegation(ctx context.Context, token string, ownerID uint64, ownerLogin string, sections []string, expires *time.Time) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		c := r.client.Delegation.Create().
			SetToken(token).
			SetOwnerID(ownerID).
			SetOwnerLogin(ownerLogin).
			SetSections(sections)
		if expires != nil {
			c = c.SetExpiresAt(*expires)
		}
		_, err := c.Save(ctx)
		return err
	})
}

func (r *Users) GetDelegation(ctx context.Context, token string) (DelegationView, error) {
	d, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(delegation.TokenEQ(token)).
			Only(ctx)
	})
	if err != nil {
		return DelegationView{}, err
	}
	return toDelegationView(d), nil
}

func (r *Users) ConsumeDelegation(ctx context.Context, token string, delegateID uint64, delegateLogin string) (DelegationView, error) {
	now := time.Now()

	d, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(delegation.TokenEQ(token)).
			Only(ctx)
	})
	if err != nil {
		return DelegationView{}, err
	}
	if d.OwnerID == delegateID {
		return DelegationView{}, errors.New("cannot delegate to yourself")
	}
	if d.ConsumedAt != nil {
		return DelegationView{}, errors.New("link already used")
	}
	if d.ExpiresAt != nil && now.After(*d.ExpiresAt) {
		return DelegationView{}, errors.New("link already used")
	}

	if grant := r.reclaimExisting(ctx, d.OwnerID, delegateID, token); grant != nil {
		return toDelegationView(grant), nil
	}

	return r.bindDelegation(ctx, d, delegateID, delegateLogin, now)
}

func (r *Users) reclaimExisting(ctx context.Context, ownerID, delegateID uint64, token string) *ent.Delegation {
	grant, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.OwnerIDEQ(ownerID),
				delegation.DelegateIDEQ(delegateID),
				delegation.ConsumedAtNotNil(),
			).
			First(ctx)
	})
	if err != nil || grant == nil {
		return nil
	}
	_ = db.WithExec(ctx, func(ctx context.Context) error {
		_, derr := r.client.Delegation.Delete().Where(delegation.TokenEQ(token)).Exec(ctx)
		return derr
	})
	return grant
}

// Must stay gated on consumed_at IS NULL so concurrent racers cannot both win.
func (r *Users) bindDelegation(ctx context.Context, d *ent.Delegation, delegateID uint64, delegateLogin string, now time.Time) (DelegationView, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Update().
			Where(
				delegation.TokenEQ(d.Token),
				delegation.ConsumedAtIsNil(),
			).
			SetDelegateID(delegateID).
			SetDelegateLogin(delegateLogin).
			SetConsumedAt(now).
			Save(ctx)
	})
	if err != nil {
		return DelegationView{}, err
	}
	if n == 0 {
		return DelegationView{}, errors.New("link already used")
	}

	d.DelegateID = delegateID
	d.DelegateLogin = delegateLogin
	d.ConsumedAt = &now
	return toDelegationView(d), nil
}

func (r *Users) ListDelegationsByOwner(ctx context.Context, ownerID uint64) ([]DelegationView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(delegation.OwnerIDEQ(ownerID)).
			Order(ent.Desc(delegation.FieldCreatedAt)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]DelegationView, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDelegationView(d))
	}
	return out, nil
}

func (r *Users) ListAccessByDelegate(ctx context.Context, delegateID uint64) ([]DelegationView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.DelegateIDEQ(delegateID),
				delegation.ConsumedAtNotNil(),
			).
			Order(ent.Desc(delegation.FieldCreatedAt)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]DelegationView, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDelegationView(d))
	}
	return out, nil
}

func (r *Users) DeleteDelegationsByOwner(ctx context.Context, ownerID uint64) ([]uint64, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.OwnerIDEQ(ownerID),
				delegation.ConsumedAtNotNil(),
			).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	delegateIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		if row.DelegateID != 0 {
			delegateIDs = append(delegateIDs, row.DelegateID)
		}
	}
	err = db.WithExec(ctx, func(ctx context.Context) error {
		_, derr := r.client.Delegation.Delete().
			Where(delegation.OwnerIDEQ(ownerID)).
			Exec(ctx)
		return derr
	})
	if err != nil {
		return nil, err
	}
	return delegateIDs, nil
}

func (r *Users) UpdateDelegationSections(ctx context.Context, token string, ownerID uint64, sections []string) (uint64, error) {
	d, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.TokenEQ(token),
				delegation.OwnerIDEQ(ownerID),
			).
			Only(ctx)
	})
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, errors.New("not found")
		}
		return 0, err
	}
	var delegateID uint64
	if d.ConsumedAt != nil {
		delegateID = d.DelegateID
	}
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return d.Update().SetSections(sections).Exec(ctx)
	})
	if err != nil {
		return 0, err
	}
	return delegateID, nil
}

func (r *Users) RevokeDelegation(ctx context.Context, token string, ownerID uint64) (uint64, error) {
	d, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.TokenEQ(token),
				delegation.OwnerIDEQ(ownerID),
			).
			Only(ctx)
	})
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, errors.New("not found")
		}
		return 0, err
	}
	var delegateID uint64
	if d.ConsumedAt != nil {
		delegateID = d.DelegateID
	}
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.Delegation.DeleteOne(d).Exec(ctx)
	})
	if err != nil {
		return 0, err
	}
	return delegateID, nil
}

func (r *Users) OptOutDelegation(ctx context.Context, ownerID uint64, delegateID uint64) error {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Delete().
			Where(
				delegation.OwnerIDEQ(ownerID),
				delegation.DelegateIDEQ(delegateID),
				delegation.ConsumedAtNotNil(),
			).
			Exec(ctx)
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("not found")
	}
	return nil
}
