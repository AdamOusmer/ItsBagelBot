// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"context"
	"math"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
)

type CounterKey struct {
	Owner uint64
	Name  string
}

type CounterIncrement struct {
	Key   CounterKey
	Delta int64
}

type CounterTable struct {
	Table     string
	Owner     string
	Name      string
	Counter   string
	UpdatedAt string
}

func CounterKeysPredicate(columns []string, keys []CounterKey) *sql.Predicate {
	return sql.P(func(b *sql.Builder) {
		b.WriteByte('(').IdentComma(columns...).WriteString(") IN (")
		for i, key := range keys {
			if i > 0 {
				b.Comma()
			}
			b.WriteByte('(').Arg(key.Owner).Comma().Arg(key.Name).WriteByte(')')
		}
		b.WriteByte(')')
	})
}

func (table CounterTable) deltas(items []CounterIncrement) sql.Querier {
	return sql.ExprFunc(func(b *sql.Builder) {
		b.WriteString("CASE")
		for _, item := range items {
			b.WriteString(" WHEN ").Ident(table.Owner).WriteString(" = ").Arg(item.Key.Owner)
			b.WriteString(" AND ").Ident(table.Name).WriteString(" = ").Arg(item.Key.Name)
			b.WriteString(" THEN ").Arg(item.Delta)
		}
		b.WriteString(" ELSE 0 END")
	})
}

func IncrementCounters(ctx context.Context, driver dialect.Driver, table CounterTable, items []CounterIncrement) (int64, error) {
	if len(items) == 0 {
		return 0, nil
	}
	query := table.update(items, driver.Dialect())
	statement, args := query.Query()
	var result sql.Result
	if err := driver.Exec(ctx, statement, args, &result); err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (table CounterTable) update(items []CounterIncrement, dialectName string) sql.Querier {
	if dialectName == dialect.MySQL {
		return table.joinUpdate(items)
	}
	keys := make([]CounterKey, len(items))
	for i, item := range items {
		keys[i] = item.Key
	}
	delta := table.deltas(items)
	return sql.Dialect(dialectName).Update(table.Table).
		Set(table.Counter, sql.ExprFunc(func(b *sql.Builder) {
			b.Ident(table.Counter).WriteString(" + ").Join(delta)
		})).Set(table.UpdatedAt, time.Now()).
		Where(sql.And(CounterKeysPredicate([]string{table.Owner, table.Name}, keys), sql.P(func(b *sql.Builder) {
			// Guard before addition to prevent SQLite integer promotion and MySQL BIGINT overflow.
			b.Ident(table.Counter).WriteString(" <= ").Arg(int64(math.MaxInt64)).WriteString(" - ").Join(delta)
		})))
}

func (table CounterTable) joinUpdate(items []CounterIncrement) sql.Querier {
	c := sql.Table(table.Table).As("c")
	return sql.ExprFunc(func(b *sql.Builder) {
		b.WriteString("UPDATE ").Ident(table.Table).WriteString(" AS c JOIN (")
		for i, item := range items {
			if i > 0 {
				b.WriteString(" UNION ALL ")
			}
			b.WriteString("SELECT ").Arg(item.Key.Owner).WriteString(" AS owner, ").Arg(item.Key.Name)
			b.WriteString(" AS name, ").Arg(item.Delta).WriteString(" AS delta")
		}
		b.WriteString(") AS d ON ").Ident(c.C(table.Owner)).WriteString(" = d.owner AND ")
		b.Ident(c.C(table.Name)).WriteString(" = d.name SET ")
		b.Ident(c.C(table.Counter)).WriteString(" = ").Ident(c.C(table.Counter)).WriteString(" + d.delta, ")
		b.Ident(c.C(table.UpdatedAt)).WriteString(" = ").Arg(time.Now())
		// Guard before addition to prevent signed BIGINT overflow.
		b.WriteString(" WHERE ").Ident(c.C(table.Counter)).WriteString(" <= ").Arg(int64(math.MaxInt64)).WriteString(" - d.delta")
	})
}
