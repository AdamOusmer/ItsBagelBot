// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"

	atlas "ariga.io/atlas/sql/schema"
	entschema "entgo.io/ent/dialect/sql/schema"
)

// NaturalKeyMigration clusters the named MySQL tables on a unique index (table name to index name); an auto-increment primary key makes concurrent upserts deadlock on its tail.
func NaturalKeyMigration(keys map[string]string) []entschema.MigrateOption {
	return []entschema.MigrateOption{entschema.WithHooks(naturalPrimaryKeys(keys)), entschema.WithDiffHook(dropNaturalKeyIndexes(keys))}
}

func naturalPrimaryKeys(keys map[string]string) entschema.Hook {
	return func(next entschema.Creator) entschema.Creator {
		return entschema.CreateFunc(func(ctx context.Context, tables ...*entschema.Table) error {
			keyed := make([]*entschema.Table, len(tables))
			for i, table := range tables {
				keyed[i] = withNaturalPrimaryKey(table, keys[table.Name])
			}
			return next.Create(ctx, keyed...)
		})
	}
}

func withNaturalPrimaryKey(table *entschema.Table, indexName string) *entschema.Table {
	natural, ok := table.Index(indexName)
	if !ok {
		return table
	}
	keyed := entschema.NewTable(table.Name).SetAnnotation(table.Annotation)
	for _, column := range table.Columns {
		keyed.AddColumn(cloneColumn(column, columnIn(table.PrimaryKey, column.Name)))
	}
	for _, column := range natural.Columns {
		key, _ := keyed.Column(column.Name)
		keyed.PrimaryKey = append(keyed.PrimaryKey, key)
	}
	for _, index := range table.Indexes {
		if index != natural {
			keyed.AddIndex(index.Name, index.Unique, columnNames(index.Columns))
			kept, _ := keyed.Index(index.Name)
			kept.Annotation = index.Annotation
		}
	}
	return keyed
}

func cloneColumn(column *entschema.Column, surrogate bool) *entschema.Column {
	return &entschema.Column{
		Name:       column.Name,
		Type:       column.Type,
		SchemaType: column.SchemaType,
		Attr:       column.Attr,
		Size:       column.Size,
		Unique:     column.Unique || surrogate,
		Increment:  column.Increment,
		Nullable:   column.Nullable,
		Default:    column.Default,
		Enums:      column.Enums,
		Collation:  column.Collation,
		Comment:    column.Comment,
	}
}

func columnIn(columns []*entschema.Column, name string) bool {
	for _, column := range columns {
		if column.Name == name {
			return true
		}
	}
	return false
}

func columnNames(columns []*entschema.Column) []string {
	names := make([]string, len(columns))
	for i, column := range columns {
		names[i] = column.Name
	}
	return names
}

func dropNaturalKeyIndexes(keys map[string]string) entschema.DiffHook {
	return func(next entschema.Differ) entschema.Differ {
		return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
			changes, err := next.Diff(current, desired)
			if err != nil {
				return nil, err
			}
			for _, change := range changes {
				appendNaturalIndexDrop(current, change, keys)
			}
			return changes, nil
		})
	}
}

func appendNaturalIndexDrop(current *atlas.Schema, change atlas.Change, keys map[string]string) {
	modify, ok := change.(*atlas.ModifyTable)
	if !ok {
		return
	}
	table, ok := current.Table(modify.T.Name)
	if !ok {
		return
	}
	if index, ok := table.Index(keys[table.Name]); ok {
		modify.Changes = append(modify.Changes, &atlas.DropIndex{I: index})
	}
}
