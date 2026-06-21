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
}

func NewModules(client *ent.Client, pub message.Publisher, app *newrelic.Application, log *zap.Logger) *Modules {

	r := &Modules{
		client: client,
		views:  cache.New[[]ModuleView](cache.DefaultCapacity, modulesCacheTTL),
		pub:    pub,
		app:    app,
		log:    log,
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

	r.Invalidate(userID)
	return nil
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

// flush lands one window of coalesced changes in a single transaction, then
// invalidates the local cache and announces every change on the bus. It runs
// detached from any request, so it reports as its own background transaction.
func (r *Modules) flush(ctx context.Context, items []data.ModuleChangedDTO) error {

	txn := r.app.StartTransaction("flush modules")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		tx, err := r.client.Tx(ctx)
		if err != nil {
			return err
		}

		for _, item := range items {
			if err := upsertModule(ctx, tx, item); err != nil {
				_ = tx.Rollback()
				txn.NoticeError(err)
				return err
			}
		}

		if err := tx.Commit(); err != nil {
			txn.NoticeError(err)
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	for _, item := range items {

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

func upsertModule(ctx context.Context, tx *ent.Tx, item data.ModuleChangedDTO) error {

	updated, err := tx.Modules.Update().
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

	if err := tx.Modules.Create().
		SetUserID(item.UserID).
		SetName(item.Name).
		SetIsEnabled(item.IsEnabled).
		SetConfigs(item.Configs).
		Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_, err = tx.Modules.Update().
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
