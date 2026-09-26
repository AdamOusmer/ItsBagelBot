// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/app/db/modules/ent/predicate"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/monitor"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	modulesKeyPrefix = "modules:"

	modulesCacheTTL = 5 * time.Minute

	modulesCacheCapacity int64 = 4096

	flushInterval = 2 * time.Second
	flushMaxSize  = 256
)

type ModuleView = projection.ModuleView

type moduleKey struct {
	userID uint64
	name   string
}

type Modules struct {
	instanceResolver AccountInstanceResolver
	client           *ent.Client
	views            *cache.Cache[[]ModuleView]
	pub              bus.Publisher
	batcher          *batch.Batcher[moduleKey, data.ModuleChangedDTO]
	app              *newrelic.Application
	log              *zap.Logger
	govee            *GoveeCreds
	spotify          *SpotifyCreds
}

func NewModules(client *ent.Client, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Modules {

	r := &Modules{
		client:  client,
		views:   cache.New[[]ModuleView](modulesCacheCapacity, modulesCacheTTL),
		pub:     pub,
		app:     app,
		log:     log,
		govee:   NewGoveeCredsFromEnv(client, log),
		spotify: NewSpotifyCredsFromEnv(client, log),
	}

	r.batcher = batch.New[moduleKey, data.ModuleChangedDTO](flushInterval, flushMaxSize, r.flush, log)

	return r
}

func (r *Modules) List(ctx context.Context, userID uint64) ([]ModuleView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(modulesKeyPrefix, userID), func(ctx context.Context) ([]ModuleView, error) {
		return db.WithQuery(ctx, func(ctx context.Context) ([]ModuleView, error) {

			rows, err := r.client.Modules.Query().
				Where(modules.UserIDEQ(userID)).
				All(ctx)
			if err != nil {
				return nil, err
			}

			views := make([]ModuleView, 0, len(rows))
			for _, row := range rows {
				if row.Name == "loyalty" && r.instanceResolver != nil && accountInstance(row.Configs) == 0 {
					continue
				}
				views = append(views, ModuleView{
					AccountCreatedAt: accountInstance(row.Configs),
					Name:             row.Name,
					IsEnabled:        row.IsEnabled,
					Configs:          row.Configs,
					Revision:         row.Revision,
				})
			}

			return views, nil
		})
	})
}

func (r *Modules) Set(userID uint64, name string, enabled bool, configs codec.RawMessage) error {

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.ModuleName(name); err != nil {
		return err
	}
	if err := validate.ConfigsJSON(configs); err != nil {
		return err
	}

	if name == "loyalty" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var err error
		configs, err = r.stampLoyalty(ctx, userID, configs)
		if err != nil {
			return err
		}
	}
	r.batcher.Add(moduleKey{userID: userID, name: name}, data.ModuleChangedDTO{
		AccountCreatedAt: accountInstance(configs),
		UserID:           userID,
		Name:             name,
		IsEnabled:        enabled,
		Configs:          configs,
	})

	return nil
}

func (r *Modules) Reproject(ctx context.Context) error {

	const pageSize = 500

	afterID := 0

	for {
		rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Modules, error) {
			return r.client.Modules.Query().
				Where(modules.IDGT(afterID)).
				Order(ent.Asc(modules.FieldID)).
				Limit(pageSize).
				All(ctx)
		})
		if err != nil {
			return err
		}

		for _, row := range rows {
			if row.Name == "loyalty" && accountInstance(row.Configs) == 0 && r.instanceResolver != nil {
				continue
			}
			if err := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, data.ModuleChangedDTO{
				AccountCreatedAt: accountInstance(row.Configs),
				UserID:           row.UserID,
				Name:             row.Name,
				IsEnabled:        row.IsEnabled,
				Configs:          row.Configs,
				Revision:         row.Revision,
			}); err != nil {
				return err
			}
		}

		if len(rows) < pageSize {
			return nil
		}

		afterID = rows[len(rows)-1].ID
	}
}

func (r *Modules) DeleteAllForUser(ctx context.Context, userID uint64) error {

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Modules.Delete().
			Where(modules.UserIDEQ(userID)).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.sweepGovee(ctx, userID)
	r.sweepSpotify(ctx, userID)

	r.Invalidate(userID)
	return nil
}

func (r *Modules) Govee() *GoveeCreds { return r.govee }

func (r *Modules) Spotify() *SpotifyCreds { return r.spotify }

func (r *Modules) sweepGovee(ctx context.Context, userID uint64) {
	if r.govee == nil {
		return
	}
	if err := r.govee.ClearKey(ctx, userID); err != nil {
		r.log.Warn("modules: failed to clear govee key on user delete", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

func (r *Modules) sweepSpotify(ctx context.Context, userID uint64) {
	if r.spotify == nil {
		return
	}
	if err := r.spotify.ClearToken(ctx, userID); err != nil {
		r.log.Warn("modules: failed to clear spotify token on user delete", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

type PatchResult struct {
	Rev      int
	Conflict bool
}

func (r *Modules) Patch(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev *int) (PatchResult, error) {
	if err := validate.UserID(userID); err != nil {
		return PatchResult{}, err
	}
	if err := validate.ModuleName(name); err != nil {
		return PatchResult{}, err
	}

	if name == "loyalty" {
		blob, err := codec.Marshal(partial)
		if err != nil {
			return PatchResult{}, err
		}
		stamped, err := r.stampLoyalty(ctx, userID, blob)
		if err != nil {
			return PatchResult{}, err
		}
		partial = decodeConfig(stamped)
	}

	var (
		res     PatchResult
		blobOut []byte
	)
	err := db.WithExec(ctx, func(ctx context.Context) error {
		row, qerr := r.client.Modules.Query().
			Where(modules.UserIDEQ(userID), modules.NameEQ(name)).
			Only(ctx)
		switch {
		case ent.IsNotFound(qerr):
			res, blobOut, qerr = r.patchInsert(ctx, userID, name, enabled, partial, expectedRev)
			return qerr
		case qerr != nil:
			return qerr
		default:
			res, blobOut, qerr = r.patchUpdate(ctx, row, enabled, partial, expectedRev)
			return qerr
		}
	})
	if err != nil {
		return PatchResult{}, err
	}
	if !res.Conflict {
		r.announcePatch(ctx, userID, name, enabled, blobOut)
	}
	return res, nil
}

func (r *Modules) patchInsert(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev *int) (PatchResult, []byte, error) {
	if expectedRev != nil && *expectedRev != 0 {
		return PatchResult{Conflict: true}, nil, nil
	}
	blob, err := mergedBlob(map[string]codec.RawMessage{}, partial, 1)
	if err != nil {
		return PatchResult{}, nil, err
	}
	created, err := r.client.Modules.Create().
		SetUserID(userID).SetName(name).SetIsEnabled(enabled).
		SetConfigs(blob).SetRevision(1).
		Save(ctx)
	if err != nil {
		return PatchResult{}, nil, err
	}
	return PatchResult{Rev: created.Revision}, blob, nil
}

func (r *Modules) patchUpdate(ctx context.Context, row *ent.Modules, enabled bool, partial map[string]codec.RawMessage, expectedRev *int) (PatchResult, []byte, error) {
	if expectedRev != nil && *expectedRev != row.Revision {
		return PatchResult{Conflict: true, Rev: row.Revision}, nil, nil
	}
	cur := decodeConfig(row.Configs)
	if row.Name == "loyalty" {
		incoming := accountInstanceMap(partial)
		current := accountInstance(row.Configs)
		if current > incoming {
			return PatchResult{}, nil, errStaleAccount
		}
		if incoming != current {
			cur = map[string]codec.RawMessage{}
		}
	}
	blob, err := mergedBlob(cur, partial, row.Revision+1)
	if err != nil {
		return PatchResult{}, nil, err
	}
	affected, err := r.client.Modules.Update().
		Where(modules.UserIDEQ(row.UserID), modules.NameEQ(row.Name), modules.RevisionEQ(row.Revision)).
		SetIsEnabled(enabled).SetConfigs(blob).AddRevision(1).
		Save(ctx)
	if err != nil {
		return PatchResult{}, nil, err
	}
	if affected == 0 {
		return PatchResult{Conflict: true, Rev: row.Revision}, nil, nil
	}
	return PatchResult{Rev: row.Revision + 1}, blob, nil
}

func mergedBlob(cur, partial map[string]codec.RawMessage, rev int) ([]byte, error) {
	mergeConfig(cur, partial)
	setRev(cur, rev)
	blob, err := codec.Marshal(cur)
	if err != nil {
		return nil, err
	}
	if err := validate.ConfigsJSON(blob); err != nil {
		return nil, err
	}
	return blob, nil
}

func (r *Modules) announcePatch(ctx context.Context, userID uint64, name string, enabled bool, blob []byte) {
	r.Invalidate(userID)
	if pubErr := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, data.ModuleChangedDTO{
		AccountCreatedAt: accountInstance(blob), UserID: userID, Name: name, IsEnabled: enabled, Configs: blob, Revision: configRevision(blob),
	}); pubErr != nil {
		r.log.Error("failed to publish module patch",
			zap.Uint64("user_id", userID), zap.String("module", name), zap.Error(pubErr))
	}
}

func decodeConfig(raw []byte) map[string]codec.RawMessage {
	out := map[string]codec.RawMessage{}
	if len(raw) == 0 {
		return out
	}
	_ = codec.Unmarshal(raw, &out)
	return out
}

const revKey = "__rev"

func mergeConfig(cfg, partial map[string]codec.RawMessage) {
	for k, v := range partial {
		if k == revKey {
			continue
		}
		cfg[k] = v
	}
}

func setRev(cfg map[string]codec.RawMessage, rev int) {
	b, _ := codec.Marshal(rev)
	cfg[revKey] = b
}

func (r *Modules) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(modulesKeyPrefix, userID))
}

func (r *Modules) Close(ctx context.Context) {
	r.batcher.Close(ctx)
	r.views.Close()
}

func (r *Modules) flush(ctx context.Context, items []data.ModuleChangedDTO) error {

	txn := r.app.StartTransaction("flush modules")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)
	log := monitor.TxnLogger(ctx, r.log)

	// Fast path: the whole window lands as one INSERT ... ON DUPLICATE KEY
	// UPDATE. If that statement fails, fall back to per-item writes so one
	// unpersistable row cannot wedge the entire batch in the retry loop
	// forever (the old whole-batch rollback + requeue did exactly that).
	ordinary := make([]data.ModuleChangedDTO, 0, len(items))
	loyalty := make([]data.ModuleChangedDTO, 0, len(items))
	for _, item := range items {
		if item.Name == "loyalty" {
			loyalty = append(loyalty, item)
		} else {
			ordinary = append(ordinary, item)
		}
	}
	landed := r.upsertEach(ctx, txn, loyalty)
	if len(ordinary) > 0 {
		landed = append(landed, ordinary...)
		if err := db.WithExec(ctx, func(ctx context.Context) error {
			return bulkUpsertModules(ctx, r.client, ordinary)
		}); err != nil {
			txn.NoticeError(err)
			landed = append(landed[:len(landed)-len(ordinary)], r.upsertEach(ctx, txn, ordinary)...)
		}
	}

	for _, item := range r.persistedDTOs(ctx, landed) {

		r.Invalidate(item.UserID)

		if err := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, item); err != nil {
			// The row is committed; losing the event only delays convergence
			// until the next change or projector rebuild, so log and move on.
			log.Error("failed to publish module change",
				zap.Uint64("user_id", item.UserID),
				zap.String("module", item.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

func bulkUpsertModules(ctx context.Context, client *ent.Client, items []data.ModuleChangedDTO) error {

	builders := make([]*ent.ModulesCreate, 0, len(items))
	for _, item := range items {
		builders = append(builders, client.Modules.Create().
			SetUserID(item.UserID).
			SetName(item.Name).
			SetIsEnabled(item.IsEnabled).
			SetConfigs(item.Configs).
			SetRevision(1))
	}

	// MySQL ignores the conflict target (ON DUPLICATE KEY UPDATE is index-less);
	// SQLite (tests) requires it.
	return client.Modules.CreateBulk(builders...).
		OnConflict(entsql.ConflictColumns(modules.FieldUserID, modules.FieldName)).
		Update(func(u *ent.ModulesUpsert) {
			u.UpdateIsEnabled()
			u.UpdateConfigs()
			u.UpdateUpdatedAt()
			u.AddRevision(1)
		}).
		Exec(ctx)
}

func (r *Modules) upsertEach(ctx context.Context, txn *newrelic.Transaction, items []data.ModuleChangedDTO) []data.ModuleChangedDTO {

	landed := make([]data.ModuleChangedDTO, 0, len(items))
	for _, item := range items {
		err := db.WithExec(ctx, func(ctx context.Context) error {
			if item.Name == "loyalty" {
				return r.upsertLoyalty(ctx, item)
			}
			return upsertModule(ctx, r.client.Modules, item)
		})
		if err == nil {
			landed = append(landed, item)
			continue
		}
		txn.NoticeError(err)
		if errors.Is(err, errStaleAccount) || ent.IsValidationError(err) || ent.IsConstraintError(err) {
			r.log.Error("dropping unpersistable module change",
				zap.Uint64("user_id", item.UserID),
				zap.String("module", item.Name),
				zap.Error(err),
			)
			continue
		}
		r.log.Warn("requeueing module change after transient flush failure",
			zap.Uint64("user_id", item.UserID),
			zap.String("module", item.Name),
			zap.Error(err),
		)
		r.batcher.Requeue(moduleKey{userID: item.UserID, name: item.Name}, item)
	}
	return landed
}

func upsertModule(ctx context.Context, c *ent.ModulesClient, item data.ModuleChangedDTO) error {

	updated, err := c.Update().
		Where(
			modules.UserIDEQ(item.UserID),
			modules.NameEQ(item.Name),
		).
		SetIsEnabled(item.IsEnabled).
		SetConfigs(item.Configs).
		AddRevision(1).
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
		SetIsEnabled(item.IsEnabled).
		SetConfigs(item.Configs).
		SetRevision(1).
		Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_, err = c.Update().
				Where(
					modules.UserIDEQ(item.UserID),
					modules.NameEQ(item.Name),
				).
				SetIsEnabled(item.IsEnabled).
				SetConfigs(item.Configs).
				AddRevision(1).
				Save(ctx)
		}
		return err
	}
	return nil
}

func (r *Modules) persistedDTOs(ctx context.Context, landed []data.ModuleChangedDTO) []data.ModuleChangedDTO {
	if len(landed) == 0 {
		return nil
	}
	where := make([]predicate.Modules, 0, len(landed))
	for _, item := range landed {
		where = append(where, modules.And(modules.UserIDEQ(item.UserID), modules.NameEQ(item.Name)))
	}
	rows, err := r.client.Modules.Query().Where(modules.Or(where...)).All(ctx)
	if err != nil {
		r.log.Warn("failed to reload committed module rows", zap.Error(err))
		return nil
	}
	result := make([]data.ModuleChangedDTO, 0, len(rows))
	for _, row := range rows {
		result = append(result, data.ModuleChangedDTO{AccountCreatedAt: accountInstance(row.Configs), UserID: row.UserID, Name: row.Name, IsEnabled: row.IsEnabled, Configs: row.Configs, Revision: row.Revision})
	}
	return result
}
