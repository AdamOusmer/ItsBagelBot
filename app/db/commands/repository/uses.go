// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/commandusebatch"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/tmpl"
	"entgo.io/ent/dialect"
	"github.com/newrelic/go-agent/v3/newrelic"
)

const useMaxRows = 64
const useMaxBytes = 256 * 1024

var errReceiptRace = errors.New("command use receipt inserted concurrently")
var errUsePayload = errors.New("command use batch ID reused with different payload")
var errUseOverflow = errors.New("command use total exceeds signed int64 range")
var errUseCheck = errors.New("command use keys require classification")

type commandUseBatch struct {
	id     string
	userID uint64
	name   string
	count  int64
}

func (r *Commands) newUseGroup() *batch.Group[commandUseBatch] {
	concurrency := 1
	if r.client.Dialect() == dialect.MySQL {
		concurrency = max(1, db.QueryConcurrency()/2)
	}
	return batch.NewGroup(batch.GroupConfig[commandUseBatch]{
		Window: 3 * time.Millisecond, MaxRows: useMaxRows, MaxBytes: useMaxBytes,
		Size: useBytes, Attributable: attributableUseError, AfterCommit: r.afterUses,
		Timeout: 3 * time.Second, Concurrency: concurrency, Log: r.log,
	}, r.flushUses)
}

func useBytes(b commandUseBatch) int { return 192 + 4*(len(b.id)+len(b.name)) }

func attributableUseError(err error) bool {
	if db.RetryableTx(err, []error{errReceiptRace}) {
		return false
	}
	if errors.Is(err, errUsePayload) || errors.Is(err, errUseOverflow) {
		return true
	}
	return ent.IsConstraintError(err) || ent.IsValidationError(err)
}

func (r *Commands) RecordUse(ctx context.Context, batchID string, dto data.CommandUsedDTO) error {
	b := commandUseBatch{id: batchID, userID: dto.UserID, name: tmpl.NormalizeName(dto.Name), count: dto.Count}
	if b.count == 0 {
		b.count = 1
	}
	if err := b.validate(); err != nil {
		return err
	}
	return r.uses.Submit(ctx, b)
}

func (b commandUseBatch) validate() error {
	if err := validate.UserID(b.userID); err != nil {
		return err
	}
	if err := validate.CommandName(b.name); err != nil {
		return err
	}
	if b.id == "" || len(b.id) > 128 {
		return fmt.Errorf("command use batch ID must contain 1..128 bytes")
	}
	if b.count < 0 {
		return fmt.Errorf("command use delta must be nonnegative")
	}
	return nil
}

func (r *Commands) flushUses(ctx context.Context, items []commandUseBatch) error {
	txn := r.app.StartTransaction("flush command uses")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)
	check := false
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return db.WithTx(ctx, r.client, func(tx *ent.Tx) error {
				return commitUses(ctx, tx, items, &check)
			})
		})
	}, errReceiptRace, errUseCheck)
	monitor.NoticeError(txn, err)
	return err
}

func (r *Commands) afterUses(_ context.Context, items []commandUseBatch) {
	keys := make([]commandKey, len(items))
	for i, b := range items {
		keys[i] = commandKey{userID: b.userID, name: b.name}
	}
	r.publications.Mark(keys)
}

func commitUses(ctx context.Context, tx *ent.Tx, items []commandUseBatch, check *bool) error {
	fresh, err := unseenUses(ctx, tx, items)
	if err != nil {
		return err
	}
	rollups, err := collapseUses(fresh)
	if err != nil {
		return err
	}
	if err := insertUseReceipts(ctx, tx, fresh); err != nil {
		return err
	}
	return applyUses(ctx, tx, rollups, check)
}

func applyUses(ctx context.Context, tx *ent.Tx, rollups []commandUseBatch, check *bool) error {
	if err := classifyUseKeys(ctx, tx, rollups, *check); err != nil {
		return err
	}
	increments := make([]db.CounterIncrement, len(rollups))
	for i, b := range rollups {
		increments[i] = db.CounterIncrement{Key: db.CounterKey{Owner: b.userID, Name: b.name}, Delta: b.count}
	}
	updated, err := tx.IncrementUses(ctx, increments)
	if useUpdateComplete(err, updated, len(rollups)) || *check {
		return err
	}
	*check = true
	return errUseCheck
}

func useUpdateComplete(err error, updated int64, count int) bool {
	return err != nil || updated == int64(count)
}

func classifyUseKeys(ctx context.Context, tx *ent.Tx, rollups []commandUseBatch, check bool) error {
	if !check {
		return nil
	}
	for _, b := range rollups {
		if err := b.checkOverflow(ctx, tx); err != nil {
			return err
		}
	}
	return nil
}

func unseenUses(ctx context.Context, tx *ent.Tx, items []commandUseBatch) ([]commandUseBatch, error) {
	ids := make([]string, len(items))
	for i, b := range items {
		ids[i] = b.id
	}
	receipts, err := tx.CommandUseBatch.Query().Where(commandusebatch.IDIn(ids...)).All(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]commandUseBatch, len(items))
	for _, row := range receipts {
		seen[row.ID] = commandUseBatch{id: row.ID, userID: row.UserID, name: row.Name, count: row.Count}
	}
	return filterUses(items, seen)
}

func filterUses(items []commandUseBatch, seen map[string]commandUseBatch) ([]commandUseBatch, error) {
	fresh := make([]commandUseBatch, 0, len(items))
	for _, b := range items {
		previous, exists := seen[b.id]
		if exists && previous != b {
			return nil, errUsePayload
		}
		if exists {
			continue
		}
		seen[b.id] = b
		fresh = append(fresh, b)
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i].id < fresh[j].id })
	return fresh, nil
}

func insertUseReceipts(ctx context.Context, tx *ent.Tx, items []commandUseBatch) error {
	if len(items) == 0 {
		return nil
	}
	builders := make([]*ent.CommandUseBatchCreate, len(items))
	for i, b := range items {
		builders[i] = tx.CommandUseBatch.Create().SetID(b.id).SetUserID(b.userID).SetName(b.name).SetCount(b.count)
	}
	err := tx.CommandUseBatch.CreateBulk(builders...).Exec(ctx)
	if ent.IsConstraintError(err) {
		return errors.Join(errReceiptRace, err)
	}
	return err
}

func collapseUses(items []commandUseBatch) ([]commandUseBatch, error) {
	totals := make(map[commandKey]int64)
	for _, b := range items {
		key := commandKey{userID: b.userID, name: b.name}
		if totals[key] > data.MaxCounter-b.count {
			return nil, errUseOverflow
		}
		totals[key] += b.count
	}
	out := make([]commandUseBatch, 0, len(totals))
	for key, count := range totals {
		out = append(out, commandUseBatch{userID: key.userID, name: key.name, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].userID == out[j].userID {
			return out[i].name < out[j].name
		}
		return out[i].userID < out[j].userID
	})
	return out, nil
}

func (b commandUseBatch) checkOverflow(ctx context.Context, tx *ent.Tx) error {
	query := tx.Commands.Query().Where(commands.UserIDEQ(b.userID), commands.NameEQ(b.name))
	row, err := query.Clone().ForUpdate().Only(ctx)
	if db.RowLocksUnsupported(err) {
		row, err = query.Only(ctx)
	}
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.Uses > data.MaxCounter-b.count {
		return errUseOverflow
	}
	return nil
}
