package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/commands"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/batch"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/newrelic/go-agent/v3/newrelic"

	"go.uber.org/zap"
)

const (
	commandsKeyPrefix = "commands:"

	commandsCacheTTL = 5 * time.Minute

	flushInterval = 2 * time.Second
	flushMaxSize  = 256
)

// CommandView is the read model for one custom command of one user.
type CommandView struct {
	Name     string `json:"name"`
	Response string `json:"response"`
	IsActive bool   `json:"is_active"`
}

type commandKey struct {
	userID uint64
	name   string
}

// Commands persists the custom chat commands. Edits are write-behind through
// the coalescing batcher (a streamer iterating on a command's wording costs
// one row write per flush window); deletions are immediate so a removed
// command stops firing right away.
type Commands struct {
	client  *ent.Client
	views   *cache.Cache[[]CommandView]
	pub     message.Publisher
	batcher *batch.Batcher[commandKey, data.CommandChangedDTO]
	app     *newrelic.Application
	log     *zap.Logger
}

func NewCommands(client *ent.Client, pub message.Publisher, app *newrelic.Application, log *zap.Logger) *Commands {

	r := &Commands{
		client: client,
		views:  cache.New[[]CommandView](commandsCacheTTL),
		pub:    pub,
		app:    app,
		log:    log,
	}

	r.batcher = batch.New[commandKey, data.CommandChangedDTO](flushInterval, flushMaxSize, r.flush, log)

	return r
}

// List returns every command of the user from the in-process cache.
func (r *Commands) List(ctx context.Context, userID uint64) ([]CommandView, error) {

	return r.views.GetOrLoad(ctx, cache.UserKey(commandsKeyPrefix, userID), func(ctx context.Context) ([]CommandView, error) {

		rows, err := r.client.Commands.Query().
			Where(commands.UserIDEQ(userID)).
			All(ctx)
		if err != nil {
			return nil, err
		}

		views := make([]CommandView, len(rows))
		for i, row := range rows {
			views[i] = CommandView{
				Name:     row.Name,
				Response: row.Response,
				IsActive: row.IsActive,
			}
		}

		return views, nil
	})
}

// Upsert validates and queues a command create or edit. Consecutive edits of
// the same command coalesce into the latest state before the next flush.
func (r *Commands) Upsert(userID uint64, name string, response string, isActive bool) error {

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}
	if err := validate.CommandResponse(response); err != nil {
		return err
	}

	r.batcher.Add(commandKey{userID: userID, name: name}, data.CommandChangedDTO{
		UserID:   userID,
		Name:     name,
		Response: response,
		IsActive: isActive,
	})

	return nil
}

// Delete removes a command immediately and announces it.
func (r *Commands) Delete(ctx context.Context, userID uint64, name string) error {

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}

	if _, err := r.client.Commands.Delete().
		Where(
			commands.UserIDEQ(userID),
			commands.NameEQ(name),
		).
		Exec(ctx); err != nil {
		return err
	}

	r.Invalidate(userID)

	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID:  userID,
		Name:    name,
		Deleted: true,
	})
}

// Invalidate drops the cached view of one user; called when a change event
// arrives from another instance of this service.
func (r *Commands) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(commandsKeyPrefix, userID))
}

// Close flushes pending writes and stops the background machinery.
func (r *Commands) Close(ctx context.Context) {
	r.batcher.Close(ctx)
	r.views.Close()
}

// flush runs detached from any request, so it reports as its own background
// transaction.
func (r *Commands) flush(ctx context.Context, items []data.CommandChangedDTO) error {

	txn := r.app.StartTransaction("flush commands")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)

	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}

	for _, item := range items {
		if err := upsertCommand(ctx, tx, item); err != nil {
			_ = tx.Rollback()
			txn.NoticeError(err)
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		txn.NoticeError(err)
		return err
	}

	for _, item := range items {

		r.Invalidate(item.UserID)

		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, item); err != nil {
			r.log.Error("failed to publish command change",
				zap.Uint64("user_id", item.UserID),
				zap.String("command", item.Name),
				zap.Error(err),
			)
		}
	}

	return nil
}

func upsertCommand(ctx context.Context, tx *ent.Tx, item data.CommandChangedDTO) error {

	updated, err := tx.Commands.Update().
		Where(
			commands.UserIDEQ(item.UserID),
			commands.NameEQ(item.Name),
		).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		Save(ctx)
	if err != nil {
		return err
	}

	if updated > 0 {
		return nil
	}

	return tx.Commands.Create().
		SetUserID(item.UserID).
		SetName(item.Name).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		Exec(ctx)
}
