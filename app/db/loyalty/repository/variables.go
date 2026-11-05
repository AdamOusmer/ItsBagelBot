// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/app/db/loyalty/ent/predicate"
	"ItsBagelBot/app/db/loyalty/ent/variable"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"
)

const (
	variableWriteAttempts = 8
	maxVariableReadNames  = 100
)

var (
	ErrVariableLimit = errors.New("variable limit reached")
	errVariableStale = errors.New("variable changed concurrently")
)

type VariableOwner struct {
	UserID   uint64
	ViewerID uint64
	Scope    string
}

func (o VariableOwner) valid() error {
	if o.UserID == 0 {
		return fmt.Errorf("%w: user_id", ErrInvalidInput)
	}
	switch o.Scope {
	case automation.VariableScopeChannel:
		return validChannelOwner(o)
	case automation.VariableScopeViewer:
		return validViewerOwner(o)
	}
	return fmt.Errorf("%w: scope", ErrInvalidInput)
}

func validChannelOwner(o VariableOwner) error {
	if o.ViewerID != 0 {
		return fmt.Errorf("%w: channel variables take no viewer", ErrInvalidInput)
	}
	return nil
}

func validViewerOwner(o VariableOwner) error {
	if o.ViewerID == 0 {
		return fmt.Errorf("%w: viewer_id", ErrInvalidInput)
	}
	return nil
}

func validVariableName(raw string) (string, error) {
	name, ok := automation.VariableName(raw)
	if !ok {
		return "", fmt.Errorf("%w: variable name", ErrInvalidInput)
	}
	return name, nil
}

func ownerPredicates(o VariableOwner) []predicate.Variable {
	return []predicate.Variable{variable.UserID(o.UserID), variable.Scope(o.Scope), variable.ViewerID(o.ViewerID)}
}

// VariablesGet returns only the names that exist, keyed by lowercase name.
func (r *Loyalty) VariablesGet(ctx context.Context, o VariableOwner, names []string) (map[string]string, error) {
	if err := o.valid(); err != nil {
		return nil, err
	}
	if len(names) > maxVariableReadNames {
		return nil, fmt.Errorf("%w: too many variable names", ErrInvalidInput)
	}
	wanted, err := validVariableNames(names)
	if err != nil {
		return nil, err
	}
	rows, err := db.WithRead(ctx, func(ctx context.Context) ([]*ent.Variable, error) {
		return r.client.Variable.Query().Where(ownerPredicates(o)...).Where(variable.NameIn(wanted...)).All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Name] = row.Value
	}
	return out, nil
}

func validVariableNames(names []string) ([]string, error) {
	out := make([]string, len(names))
	for i, raw := range names {
		name, err := validVariableName(raw)
		if err != nil {
			return nil, err
		}
		out[i] = name
	}
	return out, nil
}

func (r *Loyalty) VariableSet(ctx context.Context, o VariableOwner, name, value string) error {
	if !automation.ValidVariableValue(value) {
		return ErrVariableLimit
	}
	return r.mutateVariable(ctx, o, name, func(string) (string, error) { return value, nil })
}

func (r *Loyalty) VariableAdd(ctx context.Context, o VariableOwner, name string, amount int64) error {
	return r.mutateVariable(ctx, o, name, func(current string) (string, error) {
		next, ok := automation.AddToVariable(current, amount)
		if !ok {
			return "", ErrVariableLimit
		}
		return next, nil
	})
}

func (r *Loyalty) VariableDelete(ctx context.Context, o VariableOwner, rawName string) error {
	if err := o.valid(); err != nil {
		return err
	}
	name, err := validVariableName(rawName)
	if err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Variable.Delete().Where(ownerPredicates(o)...).Where(variable.Name(name)).Exec(ctx)
		return err
	})
}

func (r *Loyalty) mutateVariable(ctx context.Context, o VariableOwner, rawName string, next func(string) (string, error)) error {
	if err := o.valid(); err != nil {
		return err
	}
	name, err := validVariableName(rawName)
	if err != nil {
		return err
	}
	for range variableWriteAttempts {
		err = db.WithExec(ctx, func(ctx context.Context) error { return r.writeVariable(ctx, o, name, next) })
		if !retryableVariableWrite(err) {
			return err
		}
	}
	return err
}

func retryableVariableWrite(err error) bool {
	if err == nil {
		return false
	}
	return ent.IsConstraintError(err) || errors.Is(err, errVariableStale) || retryableTx(err, nil)
}

func (r *Loyalty) writeVariable(ctx context.Context, o VariableOwner, name string, next func(string) (string, error)) error {
	row, err := r.client.Variable.Query().Where(ownerPredicates(o)...).Where(variable.Name(name)).Only(ctx)
	if ent.IsNotFound(err) {
		return r.createVariable(ctx, o, name, next)
	}
	if err != nil {
		return err
	}
	value, err := next(row.Value)
	if err != nil {
		return err
	}
	return r.swapVariable(ctx, row, value)
}

func (r *Loyalty) swapVariable(ctx context.Context, row *ent.Variable, value string) error {
	updated, err := r.client.Variable.Update().
		Where(variable.ID(row.ID), variable.Revision(row.Revision)).
		SetValue(value).
		AddRevision(1).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		return errVariableStale
	}
	return nil
}

func (r *Loyalty) createVariable(ctx context.Context, o VariableOwner, name string, next func(string) (string, error)) error {
	value, err := next("")
	if err != nil {
		return err
	}
	if err := r.checkVariableRoom(ctx, o, name); err != nil {
		return err
	}
	return r.client.Variable.Create().
		SetUserID(o.UserID).SetScope(o.Scope).SetViewerID(o.ViewerID).SetName(name).SetValue(value).
		Exec(ctx)
}

// Names in use count once per channel and scope; every viewer shares the one viewer-scope name budget.
func (r *Loyalty) checkVariableRoom(ctx context.Context, o VariableOwner, name string) error {
	limit := automation.MaxChannelVariables
	if o.Scope == automation.VariableScopeViewer {
		limit = automation.MaxViewerVariableNames
	}
	others, err := r.client.Variable.Query().
		Where(variable.UserID(o.UserID), variable.Scope(o.Scope), variable.NameNEQ(name)).
		Unique(true).
		Limit(limit).
		Select(variable.FieldName).
		Strings(ctx)
	if err != nil {
		return err
	}
	if len(others) >= limit {
		return ErrVariableLimit
	}
	return nil
}
