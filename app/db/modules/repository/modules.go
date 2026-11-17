// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"ItsBagelBot/internal/domain/cooldownmarker"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/monitor"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	modulesKeyPrefix = "modules:"

	modulesCacheTTL = 5 * time.Minute

	modulesCacheCapacity int64 = 4096
)

type ModuleView = projection.ModuleView

type moduleKey struct {
	userID uint64
	name   string
}

type Modules struct {
	instanceResolver AccountInstanceResolver
	cooldownMarkers  *cooldownmarker.Store
	recipeState      *RecipeState
	client           *ent.Client
	views            *cache.Cache[[]ModuleView]
	pub              bus.Publisher
	invalidations    *bus.Invalidations
	app              *newrelic.Application
	log              *zap.Logger
	govee            *GoveeCreds
	spotify          *SpotifyCreds
	analytics        *analytics.Recorder
}

func NewModules(client *ent.Client, pub bus.Publisher, app *newrelic.Application, log *zap.Logger) *Modules {
	pub, h := bus.WithInvalidations(pub)

	r := &Modules{
		client:        client,
		views:         cache.New[[]ModuleView](modulesCacheCapacity, modulesCacheTTL, cache.StaleOnError(modulesCacheTTL)).InvalidateOn(h, cache.UserKeys(modulesKeyPrefix), data.SubjectModuleChanged, invalidate.ScopeModules),
		invalidations: h,
		pub:           pub,
		app:           app,
		log:           log,
		govee:         NewGoveeCredsFromEnv(client, log),
		spotify:       NewSpotifyCredsFromEnv(client, log, h),

		recipeState: &RecipeState{client: client},
	}

	return r
}

type ModuleWrite struct {
	UserID  uint64
	Name    string
	Enabled bool
	Configs codec.RawMessage
}

func (r *Modules) SetNow(ctx context.Context, write ModuleWrite) ([]ModuleView, error) {
	userID, name := write.UserID, write.Name
	item, err := r.moduleChange(ctx, write)
	if err != nil {
		return nil, err
	}
	if err := db.WithExec(ctx, func(ctx context.Context) error { return r.persistModuleChange(ctx, item) }); err != nil {
		return nil, err
	}
	views, err := r.loadModuleViews(ctx, userID)
	view, found := moduleNamed(views, name)
	if !found {
		r.evict(userID)
		return views, err
	}
	r.publishCommittedModule(ctx, data.ModuleChangedDTO{
		UserID: userID, Name: view.Name, IsEnabled: view.IsEnabled,
		Configs: view.Configs, Revision: view.Revision, AccountCreatedAt: view.AccountCreatedAt,
	}, monitor.TxnLogger(ctx, r.log))
	return views, nil
}

func moduleNamed(views []ModuleView, name string) (ModuleView, bool) {
	for _, view := range views {
		if view.Name == name {
			return view, true
		}
	}
	return ModuleView{}, false
}

func (r *Modules) moduleChange(ctx context.Context, write ModuleWrite) (data.ModuleChangedDTO, error) {
	userID, name, enabled, configs := write.UserID, write.Name, write.Enabled, write.Configs
	if err := validate.UserID(userID); err != nil {
		return data.ModuleChangedDTO{}, err
	}
	if err := validate.ModuleName(name); err != nil {
		return data.ModuleChangedDTO{}, err
	}
	if err := validate.ConfigsJSON(configs); err != nil {
		return data.ModuleChangedDTO{}, err
	}
	if name == "loyalty" {
		var err error
		configs, err = r.stampLoyalty(ctx, userID, configs)
		if err != nil {
			return data.ModuleChangedDTO{}, err
		}
	}
	return data.ModuleChangedDTO{
		AccountCreatedAt: accountInstance(configs), UserID: userID,
		Name: name, IsEnabled: enabled, Configs: configs,
	}, nil
}

func (r *Modules) Reproject(ctx context.Context) error {
	const pageSize = 500
	afterID := 0
	for {
		rows, err := r.modulePage(ctx, afterID, pageSize)
		if err != nil {
			return err
		}
		if err := r.publishModuleRows(ctx, rows); err != nil {
			return err
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

	return r.invalidations.Publish(invalidate.ScopeModules, userID)
}

func (r *Modules) evict(userIDs ...uint64) {
	for _, id := range userIDs {
		r.views.Invalidate(cache.UserKey(modulesKeyPrefix, id))
	}
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
	if err := r.spotify.ClearToken(ctx, SpotifyUserID(userID)); err != nil {
		r.log.Warn("modules: failed to clear spotify token on user delete", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

type PatchResult struct {
	Rev      int
	Conflict bool
}

func (r *Modules) Patch(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev *int) (PatchResult, error) {
	return r.patch(ctx, userID, name, enabled, partial, expectedRev, nil)
}

// Guard patch updates by row ID and revision so replaced rows conflict.
func (r *Modules) PatchExisting(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev, expectedID int) (PatchResult, error) {
	return r.patch(ctx, userID, name, enabled, partial, &expectedRev, &expectedID)
}

func (r *Modules) patch(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev, expectedID *int) (PatchResult, error) {
	if err := validate.UserID(userID); err != nil {
		return PatchResult{}, err
	}
	if err := validate.ModuleName(name); err != nil {
		return PatchResult{}, err
	}
	partial, err := r.stampModulePatch(ctx, userID, name, partial)
	if err != nil {
		return PatchResult{}, err
	}
	var res PatchResult
	var blob []byte
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return r.commitModules(ctx, []uint64{userID}, func(repo *Modules) error {
			var err error
			res, blob, err = repo.persistPatch(ctx, userID, name, enabled, partial, expectedRev, expectedID)
			return err
		})
	})
	if err != nil {
		return PatchResult{}, err
	}
	if !res.Conflict {
		r.announcePatch(ctx, ModuleWrite{UserID: userID, Name: name, Enabled: enabled, Configs: blob})
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
	cur, err := patchBaseConfig(row, partial)
	if err != nil {
		return PatchResult{}, nil, err
	}
	blob, err := mergedBlob(cur, partial, row.Revision+1)
	if err != nil {
		return PatchResult{}, nil, err
	}
	affected, err := r.client.Modules.Update().
		Where(modules.IDEQ(row.ID), modules.UserIDEQ(row.UserID), modules.NameEQ(row.Name), modules.RevisionEQ(row.Revision)).
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

func (r *Modules) announcePatch(ctx context.Context, write ModuleWrite) {
	userID, name, enabled, blob := write.UserID, write.Name, write.Enabled, write.Configs
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

func (r *Modules) loadModuleViews(ctx context.Context, userID uint64) ([]ModuleView, error) {
	return db.WithRead(ctx, func(ctx context.Context) ([]ModuleView, error) {
		rows, err := r.client.Modules.Query().Where(modules.UserIDEQ(userID)).All(ctx)
		if err != nil {
			return nil, err
		}
		return r.projectableModuleViews(rows), nil
	})
}

func (r *Modules) projectableModuleViews(rows []*ent.Modules) []ModuleView {
	views := make([]ModuleView, 0, len(rows))
	for _, row := range rows {
		if !r.mayProjectModule(row) {
			continue
		}
		views = append(views, ModuleView{AccountCreatedAt: accountInstance(row.Configs), Name: row.Name, IsEnabled: row.IsEnabled, Configs: row.Configs, Revision: row.Revision})
	}
	return views
}

func (r *Modules) mayProjectModule(row *ent.Modules) bool {
	if row.Name != "loyalty" {
		return true
	}
	if r.instanceResolver == nil {
		return true
	}
	return accountInstance(row.Configs) != 0
}

func (r *Modules) modulePage(ctx context.Context, afterID, limit int) ([]*ent.Modules, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Modules, error) {
		return r.client.Modules.Query().Where(modules.IDGT(afterID)).Order(ent.Asc(modules.FieldID)).Limit(limit).All(ctx)
	})
}

func (r *Modules) publishModuleRows(ctx context.Context, rows []*ent.Modules) error {
	for _, row := range rows {
		if err := r.publishProjectableModule(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func persistedModuleEvent(row *ent.Modules) data.ModuleChangedDTO {
	return data.ModuleChangedDTO{AccountCreatedAt: accountInstance(row.Configs), UserID: row.UserID, Name: row.Name, IsEnabled: row.IsEnabled, Configs: row.Configs, Revision: row.Revision}
}

func (r *Modules) stampModulePatch(ctx context.Context, id uint64, name string, partial map[string]codec.RawMessage) (map[string]codec.RawMessage, error) {
	if name != "loyalty" {
		return partial, nil
	}
	blob, err := codec.Marshal(partial)
	if err != nil {
		return nil, err
	}
	stamped, err := r.stampLoyalty(ctx, id, blob)
	if err != nil {
		return nil, err
	}
	return decodeConfig(stamped), nil
}

func (r *Modules) persistPatch(ctx context.Context, id uint64, name string, enabled bool, partial map[string]codec.RawMessage, expectedRev, expectedID *int) (PatchResult, []byte, error) {
	row, err := r.client.Modules.Query().Where(modules.UserIDEQ(id), modules.NameEQ(name)).Only(ctx)
	switch {
	case ent.IsNotFound(err):
		if expectedID != nil {
			return PatchResult{Conflict: true}, nil, nil
		}
		return r.patchInsert(ctx, id, name, enabled, partial, expectedRev)
	case err != nil:
		return PatchResult{}, nil, err
	default:
		if !patchIdentityMatches(row, partial, expectedID) {
			// Guarded maintenance must not reset settings owned by a different account incarnation.
			return PatchResult{Conflict: true, Rev: row.Revision}, nil, nil
		}
		return r.patchUpdate(ctx, row, enabled, partial, expectedRev)
	}
}

func patchIdentityMatches(row *ent.Modules, partial map[string]codec.RawMessage, expectedID *int) bool {
	if expectedID == nil {
		return true
	}
	if row.ID != *expectedID {
		return false
	}
	if row.Name != "loyalty" {
		return true
	}
	return accountInstance(row.Configs) == accountInstanceMap(partial)
}

func patchBaseConfig(row *ent.Modules, partial map[string]codec.RawMessage) (map[string]codec.RawMessage, error) {
	cur := decodeConfig(row.Configs)
	if row.Name != "loyalty" {
		return cur, nil
	}
	incoming := accountInstanceMap(partial)
	current := accountInstance(row.Configs)
	if current > incoming {
		return nil, errStaleAccount
	}
	if incoming != current {
		return map[string]codec.RawMessage{}, nil
	}
	return cur, nil
}

func (r *Modules) publishProjectableModule(ctx context.Context, row *ent.Modules) error {
	if !r.mayProjectModule(row) {
		return nil
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, persistedModuleEvent(row))
}

func (r *Modules) List(ctx context.Context, userID uint64) ([]ModuleView, error) {
	return r.views.GetOrLoad(ctx, cache.UserKey(modulesKeyPrefix, userID), func(ctx context.Context) ([]ModuleView, error) {
		return r.loadModuleViews(ctx, userID)
	})
}

func (r *Modules) Close(ctx context.Context) {
	r.views.Close()
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

func (r *Modules) publishCommittedModule(ctx context.Context, item data.ModuleChangedDTO, log *zap.Logger) {
	if err := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, item); err != nil {
		// SQL is committed; a lost notification delays convergence until hydration.
		log.Error("failed to publish module change", zap.Uint64("user_id", item.UserID), zap.String("module", item.Name), zap.Error(err))
	}
}

func (r *Modules) persistModuleChange(ctx context.Context, item data.ModuleChangedDTO) error {
	return r.commitModules(ctx, []uint64{item.UserID}, func(repo *Modules) error {
		return repo.writeModuleChange(ctx, item)
	})
}

func (r *Modules) writeModuleChange(ctx context.Context, item data.ModuleChangedDTO) error {
	if item.Name == "loyalty" {
		return r.upsertLoyalty(ctx, item)
	}
	return upsertModule(ctx, r.client.Modules, item)
}
