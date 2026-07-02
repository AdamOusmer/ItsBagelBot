package repository

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/commands"
	"ItsBagelBot/app/commands/ent/predicate"
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

// normalizeName is the canonical command key: the bare trigger, lower-cased,
// with any leading "!" dropped. Applied at every write/lookup so the DB, the
// change events, the projection and the worker's lookup all agree (chat carries
// the "!" to invoke; the stored key never does).
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
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

	flushInterval = 2 * time.Second
	flushMaxSize  = 256

	// Use-counter flush cadence. Counters are loss-tolerant, so the window can
	// be generous: one UPDATE ... uses = uses + n per hot command per window,
	// and one change event per affected command so the projection + consoles
	// pick the new count up through the normal pipeline.
	usesFlushInterval = 30 * time.Second
	usesFlushMaxKeys  = 512
)

// CommandView is the read model for one custom command of one user.
type CommandView = projection.CommandView

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

	// use-counter accumulator: RecordUse sums here; flushUses drains on a
	// ticker (or when the key set grows large) into uses = uses + n updates.
	usesMu     sync.Mutex
	usesPend   map[commandKey]uint64
	usesTicker *time.Ticker
	usesDone   chan struct{}
}

func NewCommands(client *ent.Client, pub message.Publisher, app *newrelic.Application, log *zap.Logger) *Commands {

	r := &Commands{
		client:   client,
		views:    cache.New[[]CommandView](cache.DefaultCapacity, commandsCacheTTL),
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

// List returns every command of the user from the in-process cache.
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
				}
			}

			return views, nil
		})
	})
}

// Upsert validates and queues a command create or edit. Consecutive edits of
// the same command coalesce into the latest state before the next flush.
func (r *Commands) Upsert(userID uint64, name string, aliases []string, response string, isActive bool, streamOnlineOnly bool, perm string, cooldown uint, allowedUserID uint64) error {

	name = normalizeName(name)
	aliases = normalizeAliases(aliases)

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(name); err != nil {
		return err
	}
	if err := validate.CommandAliases(aliases); err != nil {
		return err
	}
	if err := validate.CommandResponse(response); err != nil {
		return err
	}
	if err := validate.Perm(perm); err != nil {
		return err
	}
	if err := validate.Cooldown(cooldown); err != nil {
		return err
	}

	r.batcher.Add(commandKey{userID: userID, name: name}, data.CommandChangedDTO{
		UserID:           userID,
		Name:             name,
		Aliases:          aliases,
		Response:         response,
		IsActive:         isActive,
		StreamOnlineOnly: streamOnlineOnly,
		Perm:             perm,
		Cooldown:         cooldown,
		AllowedUserID:    allowedUserID,
	})

	return nil
}

// Rename changes a command's key (name) in place, preserving the row, and
// updates its other fields in the same write. Done immediately (not
// write-behind) because the batcher coalesces by (user, name); a name change
// can't be represented as a queued edit of the old key. Emits a delete for the
// old name and a change for the new so name-keyed consumers (projector, bot)
// drop the stale entry and pick up the renamed command.
func (r *Commands) Rename(ctx context.Context, userID uint64, oldName, newName string, aliases []string, response string, isActive bool, streamOnlineOnly bool, perm string, cooldown uint, allowedUserID uint64) error {

	oldName = normalizeName(oldName)
	newName = normalizeName(newName)
	aliases = normalizeAliases(aliases)

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.CommandName(oldName); err != nil {
		return err
	}
	if err := validate.CommandName(newName); err != nil {
		return err
	}
	if err := validate.CommandAliases(aliases); err != nil {
		return err
	}
	if err := validate.CommandResponse(response); err != nil {
		return err
	}
	if err := validate.Perm(perm); err != nil {
		return err
	}
	if err := validate.Cooldown(cooldown); err != nil {
		return err
	}

	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.Commands.Update().
			Where(
				commands.UserIDEQ(userID),
				commands.NameEQ(oldName),
			).
			SetName(newName).
			SetAliases(aliases).
			SetResponse(response).
			SetIsActive(isActive).
			SetStreamOnlineOnly(streamOnlineOnly).
			SetPerm(perm).
			SetCooldown(cooldown).
			SetAllowedUserID(allowedUserID).
			Save(ctx)
	})
	if err != nil {
		return err
	}

	// Old row absent (already renamed/deleted elsewhere): fall back to a plain
	// write of the new command so the edit is not lost.
	if updated == 0 {
		return r.Upsert(userID, newName, aliases, response, isActive, streamOnlineOnly, perm, cooldown, allowedUserID)
	}

	r.Invalidate(userID)

	if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, data.CommandChangedDTO{
		UserID:  userID,
		Name:    oldName,
		Deleted: true,
	}); err != nil {
		return err
	}

	// The rename preserved the row, so its uses counter survives; carry it on
	// the event so the projection doesn't regress it to zero.
	changed := data.CommandChangedDTO{
		UserID:           userID,
		Name:             newName,
		Aliases:          aliases,
		Response:         response,
		IsActive:         isActive,
		StreamOnlineOnly: streamOnlineOnly,
		Perm:             perm,
		Cooldown:         cooldown,
		AllowedUserID:    allowedUserID,
	}
	if states, serr := r.rowStates(ctx, []commandKey{{userID: userID, name: newName}}); serr == nil {
		if s, ok := states[commandKey{userID: userID, name: newName}]; ok {
			changed.Uses = s.Uses
		}
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, changed)
}

// Delete removes a command immediately and announces it.
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

// DeleteAllForUser removes every command belonging to the user and drops the
// cached view. Called when the user-deleted event arrives; idempotent — deleting
// absent rows succeeds silently.
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

// Invalidate drops the cached view of one user; called when a change event
// arrives from another instance of this service.
func (r *Commands) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(commandsKeyPrefix, userID))
}

// RecordUse counts successful executions of a command in chat (the worker
// pre-aggregates, so count covers one flush window). Purely an in-memory sum;
// flushUses persists on a ticker. Over-threshold key sets flush immediately so
// a viral chat can't grow the map without bound.
func (r *Commands) RecordUse(userID uint64, name string, count uint64) {
	name = normalizeName(name)
	if userID == 0 || name == "" {
		return
	}
	if count == 0 {
		count = 1 // absent on the wire means a single execution
	}
	r.usesMu.Lock()
	r.usesPend[commandKey{userID: userID, name: name}] += count
	overflow := len(r.usesPend) >= usesFlushMaxKeys
	r.usesMu.Unlock()
	if overflow {
		go r.flushUses(context.Background())
	}
}

// flushUses drains the accumulator into uses = uses + n updates, then reloads
// the affected rows and publishes ordinary change events built from DB truth —
// the projector and the consoles pick the new counts up through the exact same
// pipeline as an edit, so nothing downstream needs a special counter path.
func (r *Commands) flushUses(ctx context.Context) {

	r.usesMu.Lock()
	if len(r.usesPend) == 0 {
		r.usesMu.Unlock()
		return
	}
	pend := r.usesPend
	r.usesPend = map[commandKey]uint64{}
	r.usesMu.Unlock()

	txn := r.app.StartTransaction("flush command uses")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)

	keys := make([]commandKey, 0, len(pend))
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		for key, n := range pend {
			_, err := r.client.Commands.Update().
				Where(
					commands.UserIDEQ(key.userID),
					commands.NameEQ(key.name),
				).
				AddUses(int64(n)). //nolint:gosec // n is a small per-window count
				Save(ctx)
			if err != nil {
				// Keep counting the rest; a single missing/deleted row must not
				// drop the whole window.
				txn.NoticeError(err)
				r.log.Warn("failed to persist command uses",
					zap.Uint64("user_id", key.userID),
					zap.String("command", key.name),
					zap.Error(err),
				)
				continue
			}
			keys = append(keys, key)
		}
		return nil
	}); err != nil {
		txn.NoticeError(err)
		return
	}

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
			continue // row deleted between update and reload
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

// rowStates loads the given command rows and renders each as a full-state
// change DTO (event-carried state transfer, including the uses counter).
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
		}
	}
	return out, nil
}

// Close flushes pending writes and stops the background machinery.
func (r *Commands) Close(ctx context.Context) {
	r.usesTicker.Stop()
	close(r.usesDone)
	r.flushUses(ctx)
	r.batcher.Close(ctx)
	r.views.Close()
}

// flush runs detached from any request, so it reports as its own background
// transaction.
func (r *Commands) flush(ctx context.Context, items []data.CommandChangedDTO) error {

	txn := r.app.StartTransaction("flush commands")
	defer txn.End()

	ctx = newrelic.NewContext(ctx, txn)

	if err := db.WithExec(ctx, func(ctx context.Context) error {
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
		return nil
	}); err != nil {
		return err
	}

	// Publish DB truth rather than the queued edit: the row keeps counters the
	// edit never carried (uses), and event-carried state transfer must not
	// regress them in the projection.
	keys := make([]commandKey, 0, len(items))
	for _, item := range items {
		keys = append(keys, commandKey{userID: item.UserID, name: item.Name})
	}
	states, serr := r.rowStates(ctx, keys)
	if serr != nil {
		r.log.Warn("failed to reload rows after flush; publishing queued state", zap.Error(serr))
	}

	for _, item := range items {

		r.Invalidate(item.UserID)

		dto := item
		if s, ok := states[commandKey{userID: item.UserID, name: item.Name}]; ok {
			dto = s
		}
		if err := bus.PublishJSON(ctx, r.pub, data.SubjectCommandChanged, dto); err != nil {
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
		SetAliases(item.Aliases).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		SetStreamOnlineOnly(item.StreamOnlineOnly).
		SetPerm(item.Perm).
		SetCooldown(item.Cooldown).
		SetAllowedUserID(item.AllowedUserID).
		Save(ctx)
	if err != nil {
		return err
	}

	if updated > 0 {
		return nil
	}

	if err := tx.Commands.Create().
		SetUserID(item.UserID).
		SetName(item.Name).
		SetAliases(item.Aliases).
		SetResponse(item.Response).
		SetIsActive(item.IsActive).
		SetStreamOnlineOnly(item.StreamOnlineOnly).
		SetPerm(item.Perm).
		SetCooldown(item.Cooldown).
		SetAllowedUserID(item.AllowedUserID).
		Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_, err = tx.Commands.Update().
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
				Save(ctx)
		}
		return err
	}
	return nil
}

// formatAllowed renders the allowed user id for the read model: empty for 0
// (no restriction) so the dashboard can treat absence uniformly.
func formatAllowed(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}
