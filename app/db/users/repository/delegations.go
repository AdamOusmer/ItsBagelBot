// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/delegation"
	"ItsBagelBot/app/db/users/ent/predicate"
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

type DelegationLink struct {
	Token      string
	OwnerID    uint64
	OwnerLogin string
	Sections   []string
	ExpiresAt  *time.Time
}

type delegationIdentity struct {
	id    uint64
	login string
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

func (r *Users) CreateDelegation(ctx context.Context, link DelegationLink) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		c := r.client.Delegation.Create().
			SetToken(link.Token).
			SetOwnerID(link.OwnerID).
			SetOwnerLogin(link.OwnerLogin).
			SetSections(link.Sections).
			SetNillableExpiresAt(link.ExpiresAt)
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

	if grant := r.reclaimExisting(ctx, d, delegateID); grant != nil {
		return toDelegationView(grant), nil
	}

	return r.bindDelegation(ctx, d, delegationIdentity{delegateID, delegateLogin}, now)
}

func (r *Users) reclaimExisting(ctx context.Context, link *ent.Delegation, delegateID uint64) *ent.Delegation {
	grant, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.OwnerIDEQ(link.OwnerID),
				delegation.DelegateIDEQ(delegateID),
				delegation.ConsumedAtNotNil(),
			).
			First(ctx)
	})
	if err != nil || grant == nil {
		return nil
	}

	_, _ = db.WithQuery(ctx, r.client.Delegation.Delete().Where(delegation.TokenEQ(link.Token)).Exec)
	return grant
}

// Must stay gated on consumed_at IS NULL so concurrent racers cannot both win.
func (r *Users) bindDelegation(ctx context.Context, d *ent.Delegation, delegate delegationIdentity, now time.Time) (DelegationView, error) {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Update().
			Where(
				delegation.TokenEQ(d.Token),
				delegation.ConsumedAtIsNil(),
			).
			SetDelegateID(delegate.id).
			SetDelegateLogin(delegate.login).
			SetConsumedAt(now).
			Save(ctx)
	})
	if err != nil {
		return DelegationView{}, err
	}
	if n == 0 {
		return DelegationView{}, errors.New("link already used")
	}

	d.DelegateID = delegate.id
	d.DelegateLogin = delegate.login
	d.ConsumedAt = &now
	return toDelegationView(d), nil
}

func (r *Users) ListDelegationsByOwner(ctx context.Context, ownerID uint64) ([]DelegationView, error) {
	return r.listDelegations(ctx, delegation.OwnerIDEQ(ownerID))
}

func (r *Users) ListAccessByDelegate(ctx context.Context, delegateID uint64) ([]DelegationView, error) {
	return r.listDelegations(ctx, delegation.And(delegation.DelegateIDEQ(delegateID), delegation.ConsumedAtNotNil()))
}

func (r *Users) listDelegations(ctx context.Context, filter predicate.Delegation) ([]DelegationView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(filter).
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
	_, err = db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Delete().
			Where(delegation.OwnerIDEQ(ownerID)).
			Exec(ctx)
	})
	if err != nil {
		return nil, err
	}
	return delegateIDs, nil
}

func (r *Users) UpdateDelegationSections(ctx context.Context, token string, ownerID uint64, sections []string) (uint64, error) {
	return r.changeDelegation(ctx, delegationChange{token: token, ownerID: ownerID, sections: sections})
}

func (r *Users) RevokeDelegation(ctx context.Context, token string, ownerID uint64) (uint64, error) {
	return r.changeDelegation(ctx, delegationChange{token: token, ownerID: ownerID, revoke: true})
}

type delegationChange struct {
	token    string
	ownerID  uint64
	sections []string
	revoke   bool
}

func (r *Users) changeDelegation(ctx context.Context, change delegationChange) (uint64, error) {
	d, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.Delegation, error) {
		return r.client.Delegation.Query().
			Where(
				delegation.TokenEQ(change.token),
				delegation.OwnerIDEQ(change.ownerID),
			).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return 0, errors.New("not found")
	}
	if err != nil {
		return 0, err
	}
	var delegateID uint64
	if d.ConsumedAt != nil {
		delegateID = d.DelegateID
	}
	err = db.WithExec(ctx, func(ctx context.Context) error {
		if change.revoke {
			return r.client.Delegation.DeleteOne(d).Exec(ctx)
		}
		return d.Update().SetSections(change.sections).Exec(ctx)
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
