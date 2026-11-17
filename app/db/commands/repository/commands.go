// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
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
		n := tmpl.NormalizeName(a)
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

	flushInterval = 3 * time.Millisecond
	flushMaxSize  = 256
)

type CommandView = projection.CommandView

type commandKey struct {
	userID uint64
	name   string
}

type Commands struct {
	client        *ent.Client
	views         *cache.Cache[[]CommandView]
	pub           bus.Publisher
	invalidations *bus.Invalidations
	edits         *batch.Group[data.CommandChangedDTO]
	editOrder     editOrders
	uses          *batch.Group[commandUseBatch]
	publications  *batch.Dirty[commandKey]
	app           *newrelic.Application
	log           *zap.Logger

	analytics *analytics.Recorder
}

func NewCommands(client *ent.Client, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Commands {
	pub, h := bus.WithInvalidations(pub)

	r := &Commands{
		client:        client,
		views:         cache.New[[]CommandView](commandsCacheCapacity, commandsCacheTTL, cache.StaleOnError(commandsCacheTTL)).InvalidateOn(h, cache.UserKeys(commandsKeyPrefix), data.SubjectCommandChanged, invalidate.ScopeCommands),
		invalidations: h,
		pub:           pub,
		app:           app,
		log:           log,
	}

	r.edits = batch.NewGroup(batch.GroupConfig[data.CommandChangedDTO]{
		Window: flushInterval, MaxRows: flushMaxSize, MaxBytes: 256 * 1024,
		Size: commandEditBytes, Key: commandEditKey, Attributable: commandEditError,
		AfterCommit: r.afterEdits, Timeout: time.Second, QueueTimeout: 250 * time.Millisecond,
		AfterCommitTimeout: 250 * time.Millisecond, Log: log,
	}, r.flush)
	r.publications = batch.NewDirty(r.publishDirtyUses, log)
	r.uses = r.newUseGroup()

	return r
}

func (r *Commands) List(ctx context.Context, userID uint64) ([]CommandView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(commandsKeyPrefix, userID), func(ctx context.Context) ([]CommandView, error) {
		return db.WithRead(ctx, func(ctx context.Context) ([]CommandView, error) {

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
					UserCooldown:     row.UserCooldown,
					AllowedUserID:    formatAllowed(row.AllowedUserID),
					Uses:             row.Uses,
					BumpCounter:      row.BumpCounter,
					CreatedAt:        row.CreatedAt.Unix(),
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
	UserCooldown     uint
	AllowedUserID    uint64
	BumpCounter      string
}

func (s *CommandSpec) normalize() {
	s.Name = tmpl.NormalizeName(s.Name)
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
	if err := validate.Cooldown(s.UserCooldown); err != nil {
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
		UserCooldown:     s.UserCooldown,
		AllowedUserID:    s.AllowedUserID,
		BumpCounter:      s.BumpCounter,
	}
}

func (r *Commands) Upsert(userID uint64, spec CommandSpec) error {
	return r.UpsertContext(context.Background(), userID, spec)
}

func (r *Commands) UpsertContext(ctx context.Context, userID uint64, spec CommandSpec) error {

	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	unlock, err := r.editOrder.acquire(waitCtx, userID, false)
	if err != nil {
		return err
	}
	defer unlock()
	return r.edits.Submit(context.WithoutCancel(ctx), spec.dto(userID))
}

func (r *Commands) Restore(ctx context.Context, userID uint64, spec CommandSpec, uses int64) (bool, error) {

	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return false, err
	}
	if err := spec.validate(); err != nil {
		return false, err
	}

	restored, err := r.createIfAbsent(ctx, userID, spec, uses)
	if err != nil {
		return false, err
	}
	if !restored {
		return false, nil
	}

	r.analytics.Count(catalog.CustomCommands, "created")

	key := commandKey{userID: userID, name: spec.Name}
	states, serr := r.rowStates(ctx, []commandKey{key})
	if serr != nil {
		r.Invalidate(userID)
		return true, nil
	}
	return true, bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, states[key])
}

func (r *Commands) createIfAbsent(ctx context.Context, userID uint64, spec CommandSpec, uses int64) (bool, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		exists, err := r.client.Commands.Query().
			Where(commands.UserIDEQ(userID), commands.NameEQ(spec.Name)).
			Exist(ctx)
		if err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}

		b := r.client.Commands.Create().
			SetUserID(userID).
			SetName(spec.Name).
			SetUses(uses)
		applyEdit(b.Mutation(), spec.dto(userID))

		if err := b.Exec(ctx); err != nil {
			if ent.IsConstraintError(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

func (r *Commands) Rename(ctx context.Context, userID uint64, oldName string, spec CommandSpec) error {

	oldName = tmpl.NormalizeName(oldName)
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

	unlock, err := r.editOrder.acquire(ctx, userID, true)
	if err != nil {
		return err
	}
	updated, err := func() (int, error) {
		defer unlock()
		return r.renameAndPublish(ctx, userID, oldName, spec)
	}()
	if err != nil {
		return err
	}

	if updated == 0 {
		return r.UpsertContext(ctx, userID, spec)
	}
	return nil
}

func (r *Commands) renameAndPublish(ctx context.Context, userID uint64, oldName string, spec CommandSpec) (int, error) {
	updated, err := r.renameRow(ctx, userID, oldName, spec)
	if err != nil || updated == 0 {
		return updated, err
	}
	if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID: userID, Name: oldName, Deleted: true,
	}); err != nil {
		return updated, err
	}
	return updated, bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, r.renamedState(ctx, userID, spec))
}

func (r *Commands) renamedState(ctx context.Context, userID uint64, spec CommandSpec) data.CommandChangedDTO {
	key := commandKey{userID: userID, name: spec.Name}
	states, err := r.rowStates(ctx, []commandKey{key})
	if s, ok := states[key]; err == nil && ok {
		return s
	}
	return spec.dto(userID)
}

func (r *Commands) renameRow(ctx context.Context, userID uint64, oldName string, spec CommandSpec) (int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		u := r.client.Commands.Update().
			Where(
				commands.UserIDEQ(userID),
				commands.NameEQ(oldName),
			).
			SetName(spec.Name)
		applyEdit(u.Mutation(), spec.dto(userID))
		return u.Save(ctx)
	})
}

func applyEdit(m *ent.CommandsMutation, item data.CommandChangedDTO) {
	m.SetAliases(item.Aliases)
	m.SetResponse(item.Response)
	m.SetIsActive(item.IsActive)
	m.SetStreamOnlineOnly(item.StreamOnlineOnly)
	m.SetPerm(item.Perm)
	m.SetCooldown(item.Cooldown)
	m.SetUserCooldown(item.UserCooldown)
	m.SetAllowedUserID(item.AllowedUserID)
	m.SetBumpCounter(item.BumpCounter)
}

func (r *Commands) Delete(ctx context.Context, userID uint64, name string) error {

	name = tmpl.NormalizeName(name)

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}

	unlock, err := r.editOrder.acquire(ctx, userID, true)
	if err != nil {
		return err
	}
	defer unlock()
	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Commands.Delete().
			Where(
				commands.UserIDEQ(userID),
				commands.NameEQ(name),
			).
			Exec(ctx)
	})
	if err != nil {
		return err
	}

	r.analytics.Add(catalog.CustomCommands, int64(n), "deleted")

	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID:  userID,
		Name:    name,
		Deleted: true,
	})
}

func (r *Commands) DeleteAllForUser(ctx context.Context, userID uint64, cutoff time.Time) error {
	unlock, err := r.editOrder.acquire(ctx, userID, true)
	if err != nil {
		return err
	}
	defer unlock()

	n, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Commands.Delete().
			Where(commands.UserIDEQ(userID), commands.CreatedAtLTE(cutoff)).
			Exec(ctx)
	})
	if err != nil {
		return err
	}

	r.analytics.Add(catalog.CustomCommands, int64(n), "deleted")

	return r.invalidations.Publish(invalidate.ScopeCommands, userID)
}

func (r *Commands) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(commandsKeyPrefix, userID))
}

func (r *Commands) rowStates(ctx context.Context, keys []commandKey) (map[commandKey]data.CommandChangedDTO, error) {
	if len(keys) == 0 {
		return map[commandKey]data.CommandChangedDTO{}, nil
	}

	pairs := make([]db.CounterKey, len(keys))
	for i, key := range keys {
		pairs[i] = db.CounterKey{Owner: key.userID, Name: key.name}
	}

	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().Where(func(s *entsql.Selector) {
			s.Where(db.CounterKeysPredicate([]string{s.C(commands.FieldUserID), s.C(commands.FieldName)}, pairs))
		}).All(ctx)
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
			UserCooldown:     row.UserCooldown,
			AllowedUserID:    row.AllowedUserID,
			Uses:             row.Uses,
			BumpCounter:      row.BumpCounter,
			CreatedAt:        row.CreatedAt.Unix(),
		}
	}
	return out, nil
}

func (r *Commands) Close(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	if callerDeadline, ok := ctx.Deadline(); ok {
		deadline = minTime(deadline, callerDeadline)
	}
	drainCtx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	editErr := r.edits.Close(drainCtx)
	if err := r.uses.Close(drainCtx); err != nil {
		r.log.Error("command use drain failed", zap.Error(err))
		return errors.Join(editErr, err)
	}
	if editErr != nil {
		return editErr
	}
	if err := r.publications.Close(drainCtx); err != nil {
		r.log.Error("command publication drain failed", zap.Error(err))
		return err
	}
	r.views.Close()
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (r *Commands) flush(ctx context.Context, items []data.CommandChangedDTO) error {

	txn := r.app.StartTransaction("flush commands")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)
	err := db.RetryTx(ctx, func(ctx context.Context) error {
		return db.WithExec(ctx, func(ctx context.Context) error {
			return r.commitCommandBatch(ctx, items)
		})
	})
	monitor.NoticeError(txn, err)
	return err
}

func (r *Commands) afterEdits(ctx context.Context, landed []data.CommandChangedDTO) {
	log := monitor.TxnLogger(ctx, r.log)

	states, serr := r.rowStates(ctx, commandKeys(landed))
	if serr != nil {
		log.Warn("failed to reload rows after flush; publishing queued state", zap.Error(serr))
	}

	for _, item := range landed {
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

}

func upsertCommand(ctx context.Context, c *ent.CommandsClient, item data.CommandChangedDTO) (bool, error) {

	updated, err := updateCommand(ctx, c, item)
	if err != nil {
		return false, err
	}

	if updated > 0 {
		return false, nil
	}

	b := c.Create().
		SetUserID(item.UserID).
		SetName(item.Name)
	applyEdit(b.Mutation(), item)
	err = b.Exec(ctx)
	if ent.IsConstraintError(err) {
		_, err = updateCommand(ctx, c, item)
		return false, err
	}
	return err == nil, err
}

func updateCommand(ctx context.Context, c *ent.CommandsClient, item data.CommandChangedDTO) (int, error) {
	u := c.Update().
		Where(
			commands.UserIDEQ(item.UserID),
			commands.NameEQ(item.Name),
		)
	applyEdit(u.Mutation(), item)
	return u.Save(ctx)
}

func commandEditKey(item data.CommandChangedDTO) any {
	return commandKey{userID: item.UserID, name: item.Name}
}

func commandEditBytes(item data.CommandChangedDTO) int {
	size := len(item.Name) + len(item.Response) + len(item.Perm) + len(item.BumpCounter)
	for _, alias := range item.Aliases {
		size += len(alias) + 8
	}
	return 512 + 6*size
}

func commandEditError(err error) bool {
	return ent.IsValidationError(err) || ent.IsConstraintError(err)
}

func formatAllowed(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}
