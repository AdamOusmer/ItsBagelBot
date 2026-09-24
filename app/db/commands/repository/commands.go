// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/predicate"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/monitor"
	"ItsBagelBot/pkg/tmpl"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}

func normalizeResponse(response string) string {
	response = strings.ReplaceAll(response, "\r\n", "\n")
	response = strings.ReplaceAll(response, "\r", "\n")
	lines := strings.Split(response, "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func normalizeAliases(aliases []string) []string {
	if len(aliases) == 0 {
		return aliases
	}
	out := make([]string, 0, len(aliases))
	seen := map[string]struct{}{}
	for _, a := range aliases {
		n := normalizeName(a)
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

const (
	commandsKeyPrefix = "commands:"

	commandsCacheTTL = 5 * time.Minute

	commandsCacheCapacity int64 = 4096

	flushInterval = 2 * time.Second
	flushMaxSize  = 256

	usesFlushInterval = 30 * time.Second
	usesFlushMaxKeys  = 512
)

type CommandView = projection.CommandView

type commandKey struct {
	userID uint64
	name   string
}

type Commands struct {
	client  *ent.Client
	views   *cache.Cache[[]CommandView]
	pub     bus.Publisher
	batcher *batch.Batcher[commandKey, data.CommandChangedDTO]
	app     *newrelic.Application
	log     *zap.Logger

	usesMu     sync.Mutex
	usesPend   map[commandKey]uint64
	usesTicker *time.Ticker
	usesDone   chan struct{}

	usesFlushing atomic.Bool
}

func NewCommands(client *ent.Client, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Commands {

	r := &Commands{
		client:   client,
		views:    cache.New[[]CommandView](commandsCacheCapacity, commandsCacheTTL),
		pub:      pub,
		app:      app,
		log:      log,
		usesPend: map[commandKey]uint64{},
		usesDone: make(chan struct{}),
	}

	r.batcher = batch.New[commandKey, data.CommandChangedDTO](flushInterval, flushMaxSize, r.flush, log)

	r.usesTicker = time.NewTicker(usesFlushInterval)
	go func() {
		for {
			select {
			case <-r.usesTicker.C:
				r.flushUses(context.Background())
			case <-r.usesDone:
				return
			}
		}
	}()

	return r
}

func (r *Commands) List(ctx context.Context, userID uint64) ([]CommandView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(commandsKeyPrefix, userID), func(ctx context.Context) ([]CommandView, error) {
		return db.WithQuery(ctx, func(ctx context.Context) ([]CommandView, error) {

			rows, err := r.client.Commands.Query().
				Where(commands.UserIDEQ(userID)).
				All(ctx)
			if err != nil {
				return nil, err
			}

			views := make([]CommandView, len(rows))
			for i, row := range rows {
				views[i] = CommandView{
					Name:             row.Name,
					Aliases:          row.Aliases,
					Response:         row.Response,
					IsActive:         row.IsActive,
					StreamOnlineOnly: row.StreamOnlineOnly,
					Perm:             row.Perm,
					Cooldown:         row.Cooldown,
					AllowedUserID:    formatAllowed(row.AllowedUserID),
					Uses:             row.Uses,
					BumpCounter:      row.BumpCounter,
				}
			}

			return views, nil
		})
	})
}

type CommandSpec struct {
	Name             string
	Aliases          []string
	Response         string
	IsActive         bool
	StreamOnlineOnly bool
	Perm             string
	Cooldown         uint
	AllowedUserID    uint64
	BumpCounter      string
}

func (s *CommandSpec) normalize() {
	s.Name = normalizeName(s.Name)
	s.Aliases = normalizeAliases(s.Aliases)
	s.Response = normalizeResponse(s.Response)
	s.BumpCounter = tmpl.NormalizeName(s.BumpCounter)
}

func (s *CommandSpec) validate() error {
	if err := validate.CommandName(s.Name); err != nil {
		return err
	}
	if err := validate.CommandAliases(s.Aliases); err != nil {
		return err
	}
	if err := validate.CommandResponse(s.Response); err != nil {
		return err
	}
	if err := validate.Perm(s.Perm); err != nil {
		return err
	}
	if err := validate.Cooldown(s.Cooldown); err != nil {
		return err
	}
	return validate.BumpCounter(validate.CounterName(s.BumpCounter))
}

func (s *CommandSpec) dto(userID uint64) data.CommandChangedDTO {
	return data.CommandChangedDTO{
		UserID:           userID,
		Name:             s.Name,
		Aliases:          s.Aliases,
		Response:         s.Response,
		IsActive:         s.IsActive,
		StreamOnlineOnly: s.StreamOnlineOnly,
		Perm:             s.Perm,
		Cooldown:         s.Cooldown,
		AllowedUserID:    s.AllowedUserID,
		BumpCounter:      s.BumpCounter,
	}
}

func (r *Commands) Upsert(userID uint64, spec CommandSpec) error {

	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	r.batcher.Add(commandKey{userID: userID, name: spec.Name}, spec.dto(userID))

	return nil
}

func (r *Commands) Rename(ctx context.Context, userID uint64, oldName string, spec CommandSpec) error {

	oldName = normalizeName(oldName)
	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(oldName); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	updated, err := r.renameRow(ctx, userID, oldName, spec)
	if err != nil {
		return err
	}

	if updated == 0 {
		return r.Upsert(userID, spec)
	}

	r.Invalidate(userID)

	if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID:  userID,
		Name:    oldName,
		Deleted: true,
	}); err != nil {
		return err
	}

	changed := spec.dto(userID)
	key := commandKey{userID: userID, name: spec.Name}
	if states, serr := r.rowStates(ctx, []commandKey{key}); serr == nil {
		if s, ok := states[key]; ok {
			changed.Uses = s.Uses
		}
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, changed)
}

func (r *Commands) renameRow(ctx context.Context, userID uint64, oldName string, spec CommandSpec) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Commands.Update().
			Where(
				commands.UserIDEQ(userID),
				commands.NameEQ(oldName),
			).
			SetName(spec.Name).
			SetAliases(spec.Aliases).
			SetResponse(spec.Response).
			SetIsActive(spec.IsActive).
			SetStreamOnlineOnly(spec.StreamOnlineOnly).
			SetPerm(spec.Perm).
			SetCooldown(spec.Cooldown).
			SetAllowedUserID(spec.AllowedUserID).
			SetBumpCounter(spec.BumpCounter).
			Save(ctx)
	})
}

func (r *Commands) Delete(ctx context.Context, userID uint64, name string) error {

	name = normalizeName(name)

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Commands.Delete().
			Where(
				commands.UserIDEQ(userID),
				commands.NameEQ(name),
			).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.Invalidate(userID)

	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID:  userID,
		Name:    name,
		Deleted: true,
	})
}

func (r *Commands) DeleteAllForUser(ctx context.Context, userID uint64) error {

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Commands.Delete().
			Where(commands.UserIDEQ(userID)).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.Invalidate(userID)
	return nil
}

func (r *Commands) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(commandsKeyPrefix, userID))
}

func (r *Commands) RecordUse(userID uint64, name string, count uint64) {
	name = normalizeName(name)
	if userID == 0 || name == "" {
		return
	}
	if count == 0 {
		count = 1
	}
	r.usesMu.Lock()
	r.usesPend[commandKey{userID: userID, name: name}] += count
	overflow := len(r.usesPend) >= usesFlushMaxKeys
	r.usesMu.Unlock()
	if overflow && r.usesFlushing.CompareAndSwap(false, true) {
		go func() {
			defer r.usesFlushing.Store(false)
			r.flushUses(context.Background())
		}()
	}
}

func (r *Commands) flushUses(ctx context.Context) {

	pend := r.drainPendingUses()
	if len(pend) == 0 {
		return
	}

	txn := r.app.StartTransaction("flush command uses")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)

	keys, err := r.persistUses(ctx, txn, pend)
	if err != nil {
		txn.NoticeError(err)
		return
	}

	r.publishUseEvents(ctx, txn, keys)
}

func (r *Commands) drainPendingUses() map[commandKey]uint64 {
	r.usesMu.Lock()
	defer r.usesMu.Unlock()
	if len(r.usesPend) == 0 {
		return nil
	}
	pend := r.usesPend
	r.usesPend = map[commandKey]uint64{}
	return pend
}

func (r *Commands) persistUses(ctx context.Context, txn *newrelic.Transaction, pend map[commandKey]uint64) ([]commandKey, error) {
	byCount := map[uint64][]commandKey{}
	for key, n := range pend {
		byCount[n] = append(byCount[n], key)
	}
	keys := make([]commandKey, 0, len(pend))
	err := db.WithExec(ctx, func(ctx context.Context) error {
		for n, group := range byCount {
			preds := make([]predicate.Commands, 0, len(group))
			for _, key := range group {
				preds = append(preds, commands.And(
					commands.UserIDEQ(key.userID),
					commands.NameEQ(key.name),
				))
			}
			_, err := r.client.Commands.Update().
				Where(commands.Or(preds...)).
				AddUses(int64(n)). //nolint:gosec // n is a small per-window count
				Save(ctx)
			if err != nil {
				txn.NoticeError(err)
				r.log.Warn("failed to persist command uses",
					zap.Int("commands", len(group)),
					zap.Uint64("delta", n),
					zap.Error(err),
				)
				continue
			}
			keys = append(keys, group...)
		}
		return nil
	})
	return keys, err
}

func (r *Commands) publishUseEvents(ctx context.Context, txn *newrelic.Transaction, keys []commandKey) {
	states, err := r.rowStates(ctx, keys)
	if err != nil {
		txn.NoticeError(err)
		r.log.Warn("failed to load rows after uses flush", zap.Error(err))
		return
	}

	seenUsers := map[uint64]struct{}{}
	for _, key := range keys {
		if _, ok := seenUsers[key.userID]; !ok {
			seenUsers[key.userID] = struct{}{}
			r.Invalidate(key.userID)
		}
		dto, ok := states[key]
		if !ok {
			continue
		}
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, dto); err != nil {
			r.log.Error("failed to publish command uses change",
				zap.Uint64("user_id", key.userID),
				zap.String("command", key.name),
				zap.Error(err),
			)
		}
	}
}

func (r *Commands) rowStates(ctx context.Context, keys []commandKey) (map[commandKey]data.CommandChangedDTO, error) {
	if len(keys) == 0 {
		return map[commandKey]data.CommandChangedDTO{}, nil
	}

	preds := make([]predicate.Commands, 0, len(keys))
	for _, key := range keys {
		preds = append(preds, commands.And(commands.UserIDEQ(key.userID), commands.NameEQ(key.name)))
	}

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().Where(commands.Or(preds...)).All(ctx)
	})
	if err != nil {
		return nil, err
	}

	out := make(map[commandKey]data.CommandChangedDTO, len(rows))
	for _, row := range rows {
		out[commandKey{userID: row.UserID, name: row.Name}] = data.CommandChangedDTO{
			UserID:           row.UserID,
			Name:             row.Name,
			Aliases:          row.Aliases,
			Response:         row.Response,
			IsActive:         row.IsActive,
			StreamOnlineOnly: row.StreamOnlineOnly,
			Perm:             row.Perm,
			Cooldown:         row.Cooldown,
			AllowedUserID:    row.AllowedUserID,
			Uses:             row.Uses,
			BumpCounter:      row.BumpCounter,
		}
	}
	return out, nil
}

func (r *Commands) Close(ctx context.Context) {
	r.usesTicker.Stop()
	close(r.usesDone)
	r.flushUses(ctx)
	r.batcher.Close(ctx)
	r.views.Close()
}

func (r *Commands) flush(ctx context.Context, items []data.CommandChangedDTO) error {

	txn := r.app.StartTransaction("flush commands")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)
	log := monitor.TxnLogger(ctx, r.log)

	landed := items
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return bulkUpsertCommands(ctx, r.client, items)
	}); err != nil {
		txn.NoticeError(err)
		landed = r.upsertEach(ctx, txn, items)
	}

	if len(landed) == 0 {
		return nil
	}

	keys := make([]commandKey, 0, len(landed))
	for _, item := range landed {
		keys = append(keys, commandKey{userID: item.UserID, name: item.Name})
	}
	states, serr := r.rowStates(ctx, keys)
	if serr != nil {
		log.Warn("failed to reload rows after flush; publishing queued state", zap.Error(serr))
	}

	for _, item := range landed {

		r.Invalidate(item.UserID)

		dto := item
		if s, ok := states[commandKey{userID: item.UserID, name: item.Name}]; ok {
			dto = s
		}
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, dto); err != nil {
			log.Error("failed to publish command change",
				zap.Uint64("user_id", item.UserID),
				zap.String("command", item.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

func bulkUpsertCommands(ctx context.Context, client *ent.Client, items []data.CommandChangedDTO) error {

	builders := make([]*ent.CommandsCreate, 0, len(items))
	for _, item := range items {
		builders = append(builders, client.Commands.Create().
			SetUserID(item.UserID).
			SetName(item.Name).
			SetAliases(item.Aliases).
			SetResponse(item.Response).
			SetIsActive(item.IsActive).
			SetStreamOnlineOnly(item.StreamOnlineOnly).
			SetPerm(item.Perm).
			SetCooldown(item.Cooldown).
			SetAllowedUserID(item.AllowedUserID).
			SetBumpCounter(item.BumpCounter))
	}

	// On conflict, update only edit-owned columns: uses and created_at must not regress.
	return client.Commands.CreateBulk(builders...).
		OnConflict(entsql.ConflictColumns(commands.FieldUserID, commands.FieldName)).
		Update(func(u *ent.CommandsUpsert) {
			u.UpdateAliases()
			u.UpdateResponse()
			u.UpdateIsActive()
			u.UpdateStreamOnlineOnly()
			u.UpdatePerm()
			u.UpdateCooldown()
			u.UpdateAllowedUserID()
			u.UpdateBumpCounter()
			u.UpdateUpdatedAt()
		}).
		Exec(ctx)
}

func (r *Commands) upsertEach(ctx context.Context, txn *newrelic.Transaction, items []data.CommandChangedDTO) []data.CommandChangedDTO {

	landed := make([]data.CommandChangedDTO, 0, len(items))
	for _, item := range items {
		err := db.WithExec(ctx, func(ctx context.Context) error {
			return upsertCommand(ctx, r.client.Commands, item)
		})
		if err == nil {
			landed = append(landed, item)
			continue
		}
		txn.NoticeError(err)
		if ent.IsValidationError(err) || ent.IsConstraintError(err) {
			r.log.Error("dropping unpersistable command edit",
				zap.Uint64("user_id", item.UserID),
				zap.String("command", item.Name),
				zap.Error(err),
			)
			continue
		}
		r.log.Warn("requeueing command edit after transient flush failure",
			zap.Uint64("user_id", item.UserID),
			zap.String("command", item.Name),
			zap.Error(err),
		)
		r.batcher.Requeue(commandKey{userID: item.UserID, name: item.Name}, item)
	}
	return landed
}

func upsertCommand(ctx context.Context, c *ent.CommandsClient, item data.CommandChangedDTO) error {

	updated, err := c.Update().
		Where(
			commands.UserIDEQ(item.UserID),
			commands.NameEQ(item.Name),
		).
		SetAliases(item.Aliases).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		SetStreamOnlineOnly(item.StreamOnlineOnly).
		SetPerm(item.Perm).
		SetCooldown(item.Cooldown).
		SetAllowedUserID(item.AllowedUserID).
		SetBumpCounter(item.BumpCounter).
		Save(ctx)
	if err != nil {
		return err
	}

	if updated > 0 {
		return nil
	}

	if err := c.Create().
		SetUserID(item.UserID).
		SetName(item.Name).
		SetAliases(item.Aliases).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		SetStreamOnlineOnly(item.StreamOnlineOnly).
		SetPerm(item.Perm).
		SetCooldown(item.Cooldown).
		SetAllowedUserID(item.AllowedUserID).
		SetBumpCounter(item.BumpCounter).
		Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_, err = c.Update().
				Where(
					commands.UserIDEQ(item.UserID),
					commands.NameEQ(item.Name),
				).
				SetAliases(item.Aliases).
				SetResponse(item.Response).
				SetIsActive(item.IsActive).
				SetStreamOnlineOnly(item.StreamOnlineOnly).
				SetPerm(item.Perm).
				SetCooldown(item.Cooldown).
				SetAllowedUserID(item.AllowedUserID).
				SetBumpCounter(item.BumpCounter).
				Save(ctx)
		}
		return err
	}
	return nil
}

func formatAllowed(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}
