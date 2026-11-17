// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/predicate"
	"ItsBagelBot/app/db/modules/ent/variable"
	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/db"
)

const (
	maxVariableReadNames = 100
	// Outside the variable name pattern, so no recipe can read, write or count it.
	variableNamesGuard = "#names"
)

var (
	ErrVariableLimit      = errors.New("variable limit reached")
	errVariableWriteRaced = errors.New("variable write raced")
)

type VariableOwner struct {
	UserID   uint64
	ViewerID uint64
	Scope    string
}

type variableWrite struct {
	owner VariableOwner
	name  string
	next  func(string) (string, error)
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

func (r *RecipeState) VariablesGet(ctx context.Context, o VariableOwner, names []string) (map[string]string, error) {
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

func (r *RecipeState) VariableSet(ctx context.Context, o VariableOwner, name, value string) error {
	if !automation.VariableValue(value).Fits() {
		return ErrVariableLimit
	}
	return r.mutateVariable(ctx, o, name, func(string) (string, error) { return value, nil })
}

func (r *RecipeState) VariableAdd(ctx context.Context, o VariableOwner, name string, amount int64) error {
	return r.mutateVariable(ctx, o, name, func(current string) (string, error) {
		next, ok := automation.VariableValue(current).Add(amount)
		if !ok {
			return "", ErrVariableLimit
		}
		return next, nil
	})
}

func (r *RecipeState) VariableDelete(ctx context.Context, o VariableOwner, rawName string) error {
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

func (r *RecipeState) mutateVariable(ctx context.Context, o VariableOwner, rawName string, next func(string) (string, error)) error {
	if err := o.valid(); err != nil {
		return err
	}
	name, err := validVariableName(rawName)
	if err != nil {
		return err
	}
	write := variableWrite{owner: o, name: name, next: next}
	return db.RetryTx(ctx, func(ctx context.Context) error {
		err := db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error { return r.writeVariable(ctx, tx, write) })
		})
		if ent.IsConstraintError(err) {
			return fmt.Errorf("%w: %w", errVariableWriteRaced, err)
		}
		return err
	}, errVariableWriteRaced)
}

func (r *RecipeState) writeVariable(ctx context.Context, tx *ent.Tx, write variableWrite) error {
	row, err := r.variableForWrite(ctx, tx, write)
	if ent.IsNotFound(err) {
		return r.createVariable(ctx, tx, write)
	}
	if err != nil {
		return err
	}
	value, err := write.next(row.Value)
	if err != nil {
		return err
	}
	return tx.Variable.UpdateOneID(row.ID).SetValue(value).AddRevision(1).Exec(ctx)
}

func (r *RecipeState) variableForWrite(ctx context.Context, tx *ent.Tx, write variableWrite) (*ent.Variable, error) {
	query := tx.Variable.Query().Where(ownerPredicates(write.owner)...).Where(variable.Name(write.name))
	row, err := query.Clone().Only(ctx)
	if ent.IsNotFound(err) {
		return r.variableAfterGuard(ctx, tx, write)
	}
	if err != nil || r.dialect == "sqlite3" {
		return row, err
	}
	row, err = r.lockedVariable(ctx, tx.Variable.Query().Where(variable.IDEQ(row.ID)))
	if ent.IsNotFound(err) {
		return nil, errVariableWriteRaced
	}
	return row, err
}

func (r *RecipeState) variableAfterGuard(ctx context.Context, tx *ent.Tx, write variableWrite) (*ent.Variable, error) {
	if err := lockVariableNames(ctx, tx, write.owner); err != nil {
		return nil, err
	}
	return r.lockedVariable(ctx, tx.Variable.Query().Where(ownerPredicates(write.owner)...).Where(variable.Name(write.name)))
}

func (r *RecipeState) lockedVariable(ctx context.Context, query *ent.VariableQuery) (*ent.Variable, error) {
	if r.dialect != "sqlite3" {
		query.ForUpdate()
	}
	return query.Only(ctx)
}

func (r *RecipeState) createVariable(ctx context.Context, tx *ent.Tx, write variableWrite) error {
	value, err := write.next("")
	if err != nil {
		return err
	}
	if err := r.checkVariableRoom(ctx, tx, write.owner, write.name); err != nil {
		return err
	}
	return tx.Variable.Create().
		SetUserID(write.owner.UserID).SetScope(write.owner.Scope).SetViewerID(write.owner.ViewerID).SetName(write.name).SetValue(value).
		Exec(ctx)
}

// Lock the shared budget before any variable row or concurrent inserts can deadlock on gaps.
func lockVariableNames(ctx context.Context, tx *ent.Tx, o VariableOwner) error {
	return tx.Variable.Create().
		SetUserID(o.UserID).SetScope(o.Scope).SetViewerID(0).SetName(variableNamesGuard).
		OnConflictColumns(variable.FieldUserID, variable.FieldScope, variable.FieldViewerID, variable.FieldName).
		AddRevision(1).
		Exec(ctx)
}

func (r *RecipeState) checkVariableRoom(ctx context.Context, tx *ent.Tx, o VariableOwner, name string) error {
	limit := automation.MaxChannelVariables
	if o.Scope == automation.VariableScopeViewer {
		limit = automation.MaxViewerVariableNames
	}
	query := tx.Variable.Query().
		Where(variable.UserID(o.UserID), variable.Scope(o.Scope), variable.NameNotIn(name, variableNamesGuard)).
		Unique(true).
		Limit(limit)
	others, err := query.Select(variable.FieldName).Strings(ctx)
	if err != nil {
		return err
	}
	if len(others) >= limit {
		return ErrVariableLimit
	}
	return nil
}
