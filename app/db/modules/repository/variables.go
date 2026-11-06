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
	variableWriteAttempts = 8
	maxVariableReadNames  = 100
	// Outside the variable name pattern, so no recipe can read, write or count it.
	variableNamesGuard = "#names"
)

var ErrVariableLimit = errors.New("variable limit reached")

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
	for range variableWriteAttempts {
		err = db.WithExec(ctx, func(ctx context.Context) error {
			return withTx(ctx, r.client, func(tx *ent.Tx) error { return r.writeVariable(ctx, tx, o, name, next) })
		})
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
	return ent.IsConstraintError(err) || db.RetryableTx(err, nil)
}

func withTx(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *RecipeState) writeVariable(ctx context.Context, tx *ent.Tx, o VariableOwner, name string, next func(string) (string, error)) error {
	query := tx.Variable.Query().Where(ownerPredicates(o)...).Where(variable.Name(name))
	if r.dialect != "sqlite3" {
		query.ForUpdate()
	}
	row, err := query.Only(ctx)
	if ent.IsNotFound(err) {
		return createVariable(ctx, tx, o, name, next)
	}
	if err != nil {
		return err
	}
	value, err := next(row.Value)
	if err != nil {
		return err
	}
	return tx.Variable.UpdateOneID(row.ID).SetValue(value).AddRevision(1).Exec(ctx)
}

func createVariable(ctx context.Context, tx *ent.Tx, o VariableOwner, name string, next func(string) (string, error)) error {
	value, err := next("")
	if err != nil {
		return err
	}
	if err := lockVariableNames(ctx, tx, o); err != nil {
		return err
	}
	if err := checkVariableRoom(ctx, tx, o, name); err != nil {
		return err
	}
	return tx.Variable.Create().
		SetUserID(o.UserID).SetScope(o.Scope).SetViewerID(o.ViewerID).SetName(name).SetValue(value).
		Exec(ctx)
}

// READ-COMMITTED takes no gap locks, so an upsert on this row is what makes the room check and the insert one step per channel and scope.
func lockVariableNames(ctx context.Context, tx *ent.Tx, o VariableOwner) error {
	return tx.Variable.Create().
		SetUserID(o.UserID).SetScope(o.Scope).SetViewerID(0).SetName(variableNamesGuard).
		OnConflictColumns(variable.FieldUserID, variable.FieldScope, variable.FieldViewerID, variable.FieldName).
		AddRevision(1).
		Exec(ctx)
}

// Names in use count once per channel and scope; every viewer shares the one viewer-scope name budget.
func checkVariableRoom(ctx context.Context, tx *ent.Tx, o VariableOwner, name string) error {
	limit := automation.MaxChannelVariables
	if o.Scope == automation.VariableScopeViewer {
		limit = automation.MaxViewerVariableNames
	}
	others, err := tx.Variable.Query().
		Where(variable.UserID(o.UserID), variable.Scope(o.Scope), variable.NameNotIn(name, variableNamesGuard)).
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
