package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/delegation"
	"ItsBagelBot/pkg/db"
)

// DelegationView is the read model for a single-use access grant. It carries no
// secret beyond the token itself, which the owner already holds.
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

// CreateDelegation persists a fresh, unconsumed single-use grant.
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

// GetDelegation returns the grant by token (consumed or not).
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

// ConsumeDelegation binds the grant to its invitee exactly once. The update is
// gated on consumed_at IS NULL so two concurrent racers cannot both win: at most
// one UPDATE flips the row, the loser sees zero affected rows and errors out.
// Expiry is checked first (an expired link is dead even if never consumed).
//
// Reclaim: if the invitee already manages this owner's board, a second link is
// redundant. Rather than mint a duplicate grant, the redundant link is
// discarded and the grant they already hold is returned, so re-opening a fresh
// share link for the same dashboard just lands them back on it.
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

// reclaimExisting returns the invitee's current consumed grant for ownerID when
// they already hold one, after discarding the now-redundant link `token`. It
// returns nil when the invitee has no access yet (a first, real claim the
// caller must bind). The discard is best-effort: a failed delete only leaves a
// dead row to sweep later, never a duplicate live grant.
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

// bindDelegation performs the atomic single-use bind on the unconsumed row d,
// succeeding only while consumed_at is still NULL so concurrent racers cannot
// both win. The caller has already ruled out expiry and prior consumption.
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

// ListDelegationsByOwner returns every grant the owner created.
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

// ListAccessByDelegate returns the consumed grants a delegate currently holds.
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

// DeleteDelegationsByOwner removes every grant an owner created. Used when the
// owner deletes their account so no dangling links survive the user row.
func (r *Users) DeleteDelegationsByOwner(ctx context.Context, ownerID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Delegation.Delete().
			Where(delegation.OwnerIDEQ(ownerID)).
			Exec(ctx)
		return err
	})
}

// UpdateDelegationSections replaces the granted sections of a grant, scoped to
// its owner so a token alone (held by an invitee) can never re-scope someone
// else's grant. Applies to pending and consumed grants alike; a consumed grant's
// delegate picks up the change the next time they open the board.
func (r *Users) UpdateDelegationSections(ctx context.Context, token string, ownerID uint64, sections []string) error {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Update().
			Where(
				delegation.TokenEQ(token),
				delegation.OwnerIDEQ(ownerID),
			).
			SetSections(sections).
			Save(ctx)
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("not found")
	}
	return nil
}

// RevokeDelegation deletes a grant, scoped to its owner so a token alone (held
// by an invitee) can never revoke someone else's grant.
func (r *Users) RevokeDelegation(ctx context.Context, token string, ownerID uint64) error {
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Delegation.Delete().
			Where(
				delegation.TokenEQ(token),
				delegation.OwnerIDEQ(ownerID),
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

// OptOutDelegation removes a consumed grant from the delegate side. It is
// scoped to both the owner and delegate, so a delegate can only drop dashboard
// access they currently hold.
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
