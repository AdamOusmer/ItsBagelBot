package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/users/ent"
	"ItsBagelBot/app/users/ent/delegation"
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
}

// GetDelegation returns the grant by token (consumed or not).
func (r *Users) GetDelegation(ctx context.Context, token string) (DelegationView, error) {
	d, err := r.client.Delegation.Query().
		Where(delegation.TokenEQ(token)).
		Only(ctx)
	if err != nil {
		return DelegationView{}, err
	}
	return toDelegationView(d), nil
}

// ConsumeDelegation binds the grant to its invitee exactly once. The update is
// gated on consumed_at IS NULL so two concurrent racers cannot both win: at most
// one UPDATE flips the row, the loser sees zero affected rows and errors out.
// Expiry is checked first (an expired link is dead even if never consumed).
func (r *Users) ConsumeDelegation(ctx context.Context, token string, delegateID uint64, delegateLogin string) (DelegationView, error) {
	now := time.Now()

	d, err := r.client.Delegation.Query().
		Where(delegation.TokenEQ(token)).
		Only(ctx)
	if err != nil {
		return DelegationView{}, err
	}
	if d.ConsumedAt != nil {
		return DelegationView{}, errors.New("link already used")
	}
	if d.ExpiresAt != nil && now.After(*d.ExpiresAt) {
		return DelegationView{}, errors.New("link already used")
	}

	// Atomic single-use: only succeeds while consumed_at is still NULL.
	n, err := r.client.Delegation.Update().
		Where(
			delegation.TokenEQ(token),
			delegation.ConsumedAtIsNil(),
		).
		SetDelegateID(delegateID).
		SetDelegateLogin(delegateLogin).
		SetConsumedAt(now).
		Save(ctx)
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
	rows, err := r.client.Delegation.Query().
		Where(delegation.OwnerIDEQ(ownerID)).
		Order(ent.Desc(delegation.FieldCreatedAt)).
		All(ctx)
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
	rows, err := r.client.Delegation.Query().
		Where(
			delegation.DelegateIDEQ(delegateID),
			delegation.ConsumedAtNotNil(),
		).
		Order(ent.Desc(delegation.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DelegationView, 0, len(rows))
	for _, d := range rows {
		out = append(out, toDelegationView(d))
	}
	return out, nil
}

// RevokeDelegation deletes a grant, scoped to its owner so a token alone (held
// by an invitee) can never revoke someone else's grant.
func (r *Users) RevokeDelegation(ctx context.Context, token string, ownerID uint64) error {
	n, err := r.client.Delegation.Delete().
		Where(
			delegation.TokenEQ(token),
			delegation.OwnerIDEQ(ownerID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("not found")
	}
	return nil
}
