package repository

import (
	"context"
	"encoding/json"
	"time"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/modules"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/db"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	modulesKeyPrefix = "modules:"

	modulesCacheTTL = 5 * time.Minute

	flushInterval = 2 * time.Second
	flushMaxSize  = 256
)

// ModuleView is the read model for one module of one user.
type ModuleView = projection.ModuleView

type moduleKey struct {
	userID uint64
	name   string
}

// Modules persists the per-user module toggles and configs. Writes are
// write-behind: a user flipping the same switch five times in a second costs
// one row write, and a burst of changes lands as a single transaction instead
// of hammering the database once per click. Events go out only after the
// flush commits, so downstream projections never see state that failed to
// persist.
type Modules struct {
	client  *ent.Client
	views   *cache.Cache[[]ModuleView]
	pub     message.Publisher
	batcher *batch.Batcher[moduleKey, data.ModuleChangedDTO]
	app     *newrelic.Application
	log     *zap.Logger
	// govee is this service's per-broadcaster Govee API-key sub-store (sealed at
	// rest). nil when no keyset is provisioned; every use is nil-safe.
	govee *GoveeCreds
}

func NewModules(client *ent.Client, pub message.Publisher, app *newrelic.Application, log *zap.Logger) *Modules {

	r := &Modules{
		client: client,
		views:  cache.New[[]ModuleView](cache.DefaultCapacity, modulesCacheTTL),
		pub:    pub,
		app:    app,
		log:    log,
		govee:  NewGoveeCredsFromEnv(client, log),
	}

	r.batcher = batch.New[moduleKey, data.ModuleChangedDTO](flushInterval, flushMaxSize, r.flush, log)

	return r
}

// List returns every module row of the user from the in-process cache.
func (r *Modules) List(ctx context.Context, userID uint64) ([]ModuleView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(modulesKeyPrefix, userID), func(ctx context.Context) ([]ModuleView, error) {
		return db.WithQuery(ctx, func(ctx context.Context) ([]ModuleView, error) {

			rows, err := r.client.Modules.Query().
				Where(modules.UserIDEQ(userID)).
				All(ctx)
			if err != nil {
				return nil, err
			}

			views := make([]ModuleView, len(rows))
			for i, row := range rows {
				views[i] = ModuleView{
					Name:      row.Name,
					IsEnabled: row.IsEnabled,
					Configs:   row.Configs,
					Revision:  row.Revision,
				}
			}

			return views, nil
		})
	})
}

// Set validates and queues a toggle or config change. Consecutive changes to
// the same module coalesce into the latest state before the next flush.
func (r *Modules) Set(userID uint64, name string, enabled bool, configs json.RawMessage) error {

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.ModuleName(name); err != nil {
		return err
	}
	if err := validate.ConfigsJSON(configs); err != nil {
		return err
	}

	r.batcher.Add(moduleKey{userID: userID, name: name}, data.ModuleChangedDTO{
		UserID:    userID,
		Name:      name,
		IsEnabled: enabled,
		Configs:   configs,
	})

	return nil
}

// Reproject republishes the current state of every module row as ordinary
// change events, paged by row ID so the table is never loaded at once. The
// projector requests this on a cold start to rebuild the Valkey projection.
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
			if err := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, data.ModuleChangedDTO{
				UserID:    row.UserID,
				Name:      row.Name,
				IsEnabled: row.IsEnabled,
				Configs:   row.Configs,
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

// DeleteAllForUser removes every module row belonging to the user and drops the
// cached view. Called when the user-deleted event arrives; idempotent — deleting
// absent rows succeeds silently.
func (r *Modules) DeleteAllForUser(ctx context.Context, userID uint64) error {

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.Modules.Delete().
			Where(modules.UserIDEQ(userID)).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	// The govee key lives outside the module rows (sealed in its own table), so
	// it is swept here too; nil-safe and best-effort.
	r.sweepGovee(ctx, userID)

	r.Invalidate(userID)
	return nil
}

// Govee exposes the key sub-store so the RPC layer can wire its custody verbs.
// nil when govee key custody is disabled.
func (r *Modules) Govee() *GoveeCreds { return r.govee }

// sweepGovee removes a deleted user's stored Govee key. nil-safe (custody
// disabled) and best-effort: a failure is logged, never propagated, since the
// module rows are already gone and the key is unreadable without its keyset.
func (r *Modules) sweepGovee(ctx context.Context, userID uint64) {
	if r.govee == nil {
		return
	}
	if err := r.govee.ClearKey(ctx, userID); err != nil {
		r.log.Warn("modules: failed to clear govee key on user delete", zap.Uint64("user_id", userID), zap.Error(err))
	}
}

// PatchResult reports a Patch outcome: the row's revision after the attempt, and
// whether the write was rejected because the caller's expected revision was stale.
type PatchResult struct {
	Rev      int
	Conflict bool
}

// Patch merges partial config keys into a module's stored config under optimistic
// concurrency. When expectedRev is non-nil it must equal the row's current
// revision, or the write is rejected as a conflict (no mutation) so the caller can
// refetch and retry; nil skips the check (last-write-wins). On success the
// revision is bumped and the change is announced like any other write. A missing
// row is created at revision 1. Unlike Set, Patch is synchronous and does not go
// through the write-behind batcher: config edits are low-frequency, and the
// compare-and-swap needs the current row, so batching would defeat the check.
func (r *Modules) Patch(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]json.RawMessage, expectedRev *int) (PatchResult, error) {
	if err := validate.UserID(userID); err != nil {
		return PatchResult{}, err
	}
	if err := validate.ModuleName(name); err != nil {
		return PatchResult{}, err
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

// patchInsert creates a module row at revision 1. A non-zero expected revision on
// a missing row is a conflict (the row the caller thought it was editing is gone).
func (r *Modules) patchInsert(ctx context.Context, userID uint64, name string, enabled bool, partial map[string]json.RawMessage, expectedRev *int) (PatchResult, []byte, error) {
	if expectedRev != nil && *expectedRev != 0 {
		return PatchResult{Conflict: true}, nil, nil
	}
	blob, err := mergedBlob(map[string]json.RawMessage{}, partial, 1)
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

// patchUpdate merges into an existing row with a compare-and-swap on the revision:
// the write lands only if the revision is still what we read, so a concurrent
// patch that landed in between loses the race and its caller retries. Portable and
// lock-free (a conditional UPDATE, no FOR UPDATE).
func (r *Modules) patchUpdate(ctx context.Context, row *ent.Modules, enabled bool, partial map[string]json.RawMessage, expectedRev *int) (PatchResult, []byte, error) {
	if expectedRev != nil && *expectedRev != row.Revision {
		return PatchResult{Conflict: true, Rev: row.Revision}, nil, nil
	}
	blob, err := mergedBlob(decodeConfig(row.Configs), partial, row.Revision+1)
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

// mergedBlob overlays partial onto cur, stamps the revision mirror, and marshals
// the validated config blob.
func mergedBlob(cur, partial map[string]json.RawMessage, rev int) ([]byte, error) {
	mergeConfig(cur, partial)
	setRev(cur, rev)
	blob, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}
	if err := validate.ConfigsJSON(blob); err != nil {
		return nil, err
	}
	return blob, nil
}

// announcePatch drops the cached view and publishes the change, mirroring flush so
// the projection converges.
func (r *Modules) announcePatch(ctx context.Context, userID uint64, name string, enabled bool, blob []byte) {
	r.Invalidate(userID)
	if pubErr := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, data.ModuleChangedDTO{
		UserID: userID, Name: name, IsEnabled: enabled, Configs: blob,
	}); pubErr != nil {
		r.log.Error("failed to publish module patch",
			zap.Uint64("user_id", userID), zap.String("module", name), zap.Error(pubErr))
	}
}

// decodeConfig parses a stored config blob into a mutable key map; a nil or
// corrupt blob yields an empty map so a patch can still proceed.
func decodeConfig(raw []byte) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// revKey mirrors the row's revision inside the config blob so it flows to the
// dashboard through the existing config projection (the client strips it). The
// authoritative revision for the compare-and-swap is the column, not this mirror.
const revKey = "__rev"

// mergeConfig overlays partial's keys onto cfg (partial wins), ignoring any
// client-sent revision mirror — the server owns it.
func mergeConfig(cfg, partial map[string]json.RawMessage) {
	for k, v := range partial {
		if k == revKey {
			continue
		}
		cfg[k] = v
	}
}

// setRev writes the revision mirror into the config blob.
func setRev(cfg map[string]json.RawMessage, rev int) {
	b, _ := json.Marshal(rev)
	cfg[revKey] = b
}

// Invalidate drops the cached view of one user; called when a change event
// arrives from another instance of this service.
func (r *Modules) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(modulesKeyPrefix, userID))
}

// Close flushes pending writes and stops the background machinery.
func (r *Modules) Close(ctx context.Context) {
	r.batcher.Close(ctx)
	r.views.Close()
}

// flush lands one window of coalesced changes, then invalidates the local
// cache and announces every landed change on the bus. It runs detached from
// any request, so it reports as its own background transaction.
func (r *Modules) flush(ctx context.Context, items []data.ModuleChangedDTO) error {

	txn := r.app.StartTransaction("flush modules")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)

	// Fast path: the whole window lands as one INSERT ... ON DUPLICATE KEY
	// UPDATE. If that statement fails, fall back to per-item writes so one
	// unpersistable row cannot wedge the entire batch in the retry loop
	// forever (the old whole-batch rollback + requeue did exactly that).
	landed := items
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return bulkUpsertModules(ctx, r.client, items)
	}); err != nil {
		txn.NoticeError(err)
		landed = r.upsertEach(ctx, txn, items)
	}

	for _, item := range landed {

		r.Invalidate(item.UserID)

		if err := bus.PublishJSON(ctx, r.pub, data.SubjectModuleChanged, item); err != nil {
			// The row is committed; losing the event only delays convergence
			// until the next change or projector rebuild, so log and move on.
			r.log.Error("failed to publish module change",
				zap.Uint64("user_id", item.UserID),
				zap.String("module", item.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

// bulkUpsertModules lands one flush window as a single
// INSERT ... ON DUPLICATE KEY UPDATE keyed on the (user_id, name) unique index.
func bulkUpsertModules(ctx context.Context, client *ent.Client, items []data.ModuleChangedDTO) error {

	builders := make([]*ent.ModulesCreate, 0, len(items))
	for _, item := range items {
		builders = append(builders, client.Modules.Create().
			SetUserID(item.UserID).
			SetName(item.Name).
			SetIsEnabled(item.IsEnabled).
			SetConfigs(item.Configs))
	}

	// MySQL ignores the conflict target (ON DUPLICATE KEY UPDATE is index-less);
	// SQLite (tests) requires it.
	return client.Modules.CreateBulk(builders...).
		OnConflict(entsql.ConflictColumns(modules.FieldUserID, modules.FieldName)).
		Update(func(u *ent.ModulesUpsert) {
			u.UpdateIsEnabled()
			u.UpdateConfigs()
			u.UpdateUpdatedAt()
		}).
		Exec(ctx)
}

// upsertEach persists a failed window one item at a time and returns the
// items that landed. Rows the database will never accept (validation or
// constraint errors) are dropped with an error log; transiently failing rows
// are requeued into the batcher so the next window retries them without
// holding the rest of the batch hostage.
func (r *Modules) upsertEach(ctx context.Context, txn *newrelic.Transaction, items []data.ModuleChangedDTO) []data.ModuleChangedDTO {

	landed := make([]data.ModuleChangedDTO, 0, len(items))
	for _, item := range items {
		err := db.WithExec(ctx, func(ctx context.Context) error {
			return upsertModule(ctx, r.client.Modules, item)
		})
		if err == nil {
			landed = append(landed, item)
			continue
		}
		txn.NoticeError(err)
		if ent.IsValidationError(err) || ent.IsConstraintError(err) {
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
		Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_, err = c.Update().
				Where(
					modules.UserIDEQ(item.UserID),
					modules.NameEQ(item.Name),
				).
				SetIsEnabled(item.IsEnabled).
				SetConfigs(item.Configs).
				Save(ctx)
		}
		return err
	}
	return nil
}
