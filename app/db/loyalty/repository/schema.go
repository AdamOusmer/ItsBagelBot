// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"

	"ItsBagelBot/app/db/loyalty/ent"

	atlas "ariga.io/atlas/sql/schema"
	"entgo.io/ent/dialect"
	entschema "entgo.io/ent/dialect/sql/schema"
)

var naturalKeys = map[string]string{
	"counters":        "counter_user_id_name",
	"counter_entries": "counterentry_user_id_name_command_viewer_id",
}

// MigrateSchema clusters MySQL counter tables on their natural key; an auto-increment primary key makes concurrent upserts deadlock on its tail.
func MigrateSchema(ctx context.Context, client *ent.Client, dialectName string) error {
	if dialectName != dialect.MySQL {
		return client.Schema.Create(ctx)
	}
	return client.Schema.Create(ctx, entschema.WithHooks(naturalPrimaryKeys), entschema.WithDiffHook(dropNaturalKeyIndexes))
}

func naturalPrimaryKeys(next entschema.Creator) entschema.Creator {
	return entschema.CreateFunc(func(ctx context.Context, tables ...*entschema.Table) error {
		keyed := make([]*entschema.Table, len(tables))
		for i, table := range tables {
			keyed[i] = withNaturalPrimaryKey(table)
		}
		return next.Create(ctx, keyed...)
	})
}

func withNaturalPrimaryKey(table *entschema.Table) *entschema.Table {
	natural, ok := table.Index(naturalKeys[table.Name])
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

func dropNaturalKeyIndexes(next entschema.Differ) entschema.Differ {
	return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
		changes, err := next.Diff(current, desired)
		if err != nil {
			return nil, err
		}
		for _, change := range changes {
			appendNaturalIndexDrop(current, change)
		}
		return changes, nil
	})
}

func appendNaturalIndexDrop(current *atlas.Schema, change atlas.Change) {
	modify, ok := change.(*atlas.ModifyTable)
	if !ok {
		return
	}
	table, ok := current.Table(modify.T.Name)
	if !ok {
		return
	}
	if index, ok := table.Index(naturalKeys[table.Name]); ok {
		modify.Changes = append(modify.Changes, &atlas.DropIndex{I: index})
	}
}
