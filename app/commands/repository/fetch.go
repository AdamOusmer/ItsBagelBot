// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/commands"
	"ItsBagelBot/app/commands/ent/fetchdefinition"
	"ItsBagelBot/app/commands/ent/fetchkey"
	"ItsBagelBot/internal/domain/event/data"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"

	domaincrypto "ItsBagelBot/internal/domain/crypto"

	"go.uber.org/zap"
)

// ErrNoFetchKey marks a label with no sealed key on file.
var ErrNoFetchKey = errors.New("no fetch key on record")

// ErrCustodyUnavailable is answered by fetch_set_key / key reads when the
// service booted without a usable Tink keyset: definitions keep working
// keyless, custody refuses closed.
var ErrCustodyUnavailable = errors.New("key custody unavailable")

// ErrFetchDefReferenced refuses a definition delete while commands still
// embed its {urlfetch:<name>} token; the message names them.
type ErrFetchDefReferenced struct {
	Commands []string
}

func (e *ErrFetchDefReferenced) Error() string {
	return "still referenced by commands: " + strings.Join(e.Commands, ", ")
}

const (
	fetchesKeyPrefix = "fetchdefs:"

	fetchesCacheTTL = 5 * time.Minute

	// fetchesCacheCapacity ceilings the definition view cache, keyed one entry
	// per user (the whole list) like the command view cache: a few thousand
	// covers the users read within the TTL without holding
	// cache.DefaultCapacity entries resident at rest.
	fetchesCacheCapacity int64 = 4096
)

// FetchView is the read model of one definition (wire shape in fetchkey).
type FetchView = fetchkeyrpc.FetchView

// KeyView is the read model of one custody row: metadata only.
type KeyView = fetchkeyrpc.KeyView

// Fetches persists $(urlfetch) definitions and their sealed API keys.
//
// Unlike Commands this store is fully synchronous: the dashboard upsert must
// quota-count against real rows before inserting (a write-behind queue cannot
// enforce a per-broadcaster cap — its Upsert enqueues with no row check, and
// the cap would only be discoverable at flush), and authors expect the
// rehearsal path to see the saved def immediately. Definition volume is
// console-clicks, not chat-volume, so there is nothing to coalesce.
type Fetches struct {
	client *ent.Client
	packer domaincrypto.Packer // nil when key custody is disabled
	views  *cache.Cache[[]FetchView]
	pub    bus.Publisher
	log    *zap.Logger
}

// NewFetches builds the store over the shared ent client. packer may be nil:
// the service then runs with key custody disabled and answers custody verbs
// with ErrCustodyUnavailable while definitions keep working keyless.
func NewFetches(client *ent.Client, packer domaincrypto.Packer, pub bus.Publisher, log *zap.Logger) *Fetches {
	return &Fetches{
		client: client,
		packer: packer,
		views:  cache.New[[]FetchView](fetchesCacheCapacity, fetchesCacheTTL),
		pub:    pub,
		log:    log,
	}
}

// NewFetchesFromEnv builds the custody packer from the service's Tink keyset
// (TINK_KEYSET_PATH) with modules' best-effort rules, deliberately diverging
// from users' fatal MustGet: commands sits on the core chat path even with
// zero keys ever sealed, so an unset path or an absent optional mount disables
// key custody (warn) instead of crash-looping every commands pod. Only a
// present-but-invalid keyset is fatal — that is real misconfiguration.
func NewFetchesFromEnv(log *zap.Logger) domaincrypto.Packer {
	path := env.Get("TINK_KEYSET_PATH", "")
	if path == "" {
		log.Warn("fetch key custody disabled: TINK_KEYSET_PATH not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("fetch key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	packer, err := crypto.NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return packer
}

// fetchAAD binds each sealed envelope to its broadcaster AND label: "<uid>|fetch_key|<label>",
// mirroring goveeAAD. The label term stops an envelope copied onto another
// label of the same user from opening — labels are the only thing separating
// one broadcaster's keys from each other.
func fetchAAD(userID uint64, label string) []byte {
	aad := make([]byte, 0, 20+len("|fetch_key|")+len(label))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|fetch_key|"...)
	aad = append(aad, label...)
	return aad
}

// last4 derives the display suffix at seal time: the final four characters of
// the plaintext (the whole value when shorter). Display-only.
func last4(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}

// FetchSpec is the caller-editable state of one definition, as accepted by
// UpsertDef and RenameDef. Name arrives raw from the console and is
// normalized before validation.
type FetchSpec struct {
	Name     string
	URL      string
	Path     []string
	KeyLabel string
	IsActive bool
}

func (s *FetchSpec) normalize() {
	s.Name = normalizeName(s.Name)
	s.KeyLabel = strings.TrimSpace(s.KeyLabel)
}

func (s *FetchSpec) validate() error {
	if err := validate.FetchDefName(s.Name); err != nil {
		return err
	}
	if err := validate.FetchURL(s.URL); err != nil {
		return err
	}
	if err := validate.FetchPath(s.Path); err != nil {
		return err
	}
	if s.KeyLabel == "" {
		return nil
	}
	return validate.KeyLabel(s.KeyLabel)
}

func (s *FetchSpec) dto(userID uint64) data.FetchChangedDTO {
	return data.FetchChangedDTO{
		UserID:   userID,
		Name:     s.Name,
		URL:      s.URL,
		JSONPath: s.Path,
		KeyLabel: s.KeyLabel,
		IsActive: s.IsActive,
	}
}

func (s *FetchSpec) view(name string) FetchView {
	return FetchView{
		Name:     name,
		URL:      s.URL,
		JSONPath: s.Path,
		KeyLabel: s.KeyLabel,
		IsActive: s.IsActive,
	}
}

// List returns every definition of the user from the in-process cache.
func (r *Fetches) List(ctx context.Context, userID uint64) ([]FetchView, error) {
	return r.views.GetOrLoad(ctx, cache.UserKey(fetchesKeyPrefix, userID), func(ctx context.Context) ([]FetchView, error) {
		return db.WithQuery(ctx, func(ctx context.Context) ([]FetchView, error) {
			rows, err := r.client.FetchDefinition.Query().
				Where(fetchdefinition.UserIDEQ(userID)).
				All(ctx)
			if err != nil {
				return nil, err
			}
			views := make([]FetchView, len(rows))
			for i, row := range rows {
				views[i] = FetchView{
					Name:     row.Name,
					URL:      row.URL,
					JSONPath: row.JSONPath,
					KeyLabel: row.KeyLabel,
					IsActive: row.IsActive,
				}
			}
			return views, nil
		})
	})
}

// ListKeys returns the custody metadata (label + last4) of every stored key;
// values have no read path.
func (r *Fetches) ListKeys(ctx context.Context, userID uint64) ([]KeyView, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.FetchKey, error) {
		return r.client.FetchKey.Query().
			Where(fetchkey.UserIDEQ(userID)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]KeyView, len(rows))
	for i, row := range rows {
		out[i] = KeyView{Label: row.Label, Last4: row.Last4, CreatedAt: row.CreatedAt}
	}
	return out, nil
}

// UpsertDef validates, quota-counts synchronously against real rows, writes
// immediately (never through the write-behind batcher — see the Fetches doc
// comment), and announces the new state on SubjectFetchChanged so the
// projector rewrites the fetch:<name> field.
func (r *Fetches) UpsertDef(ctx context.Context, userID uint64, spec FetchSpec) error {

	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	exists, err := r.defExists(ctx, userID, spec.Name)
	if err != nil {
		return err
	}
	if !exists {
		count, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
			return r.client.FetchDefinition.Query().
				Where(fetchdefinition.UserIDEQ(userID)).
				Count(ctx)
		})
		if err != nil {
			return err
		}
		// Count-then-insert races only when one broadcaster saves two
		// different names concurrently — a single console author does not do
		// that, and the unique (user_id, name) index still holds. The cap's
		// purpose is bounding abuse fan-out, not ledger precision.
		if count >= validate.MaxFetchDefsPerBroadcaster {
			return validate.FetchDefQuotaError()
		}
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.FetchDefinition.Create().
			SetUserID(userID).
			SetName(spec.Name).
			SetURL(spec.URL).
			SetJSONPath(spec.Path).
			SetKeyLabel(spec.KeyLabel).
			SetIsActive(spec.IsActive).
			OnConflictColumns(fetchdefinition.FieldUserID, fetchdefinition.FieldName).
			Update(
				func(u *ent.FetchDefinitionUpsert) {
					u.UpdateURL()
					u.UpdateJSONPath()
					u.UpdateKeyLabel()
					u.UpdateIsActive()
					u.UpdateUpdatedAt()
				},
			).
			Exec(ctx)
	}); err != nil {
		return err
	}

	r.Invalidate(userID)

	// One audit line per mutate; never the URL query or any key material.
	r.log.Info("fetch definition saved",
		zap.Uint64("user_id", userID),
		zap.String("name", spec.Name),
	)

	return bus.PublishJSON(ctx, r.pub, data.SubjectFetchChanged, spec.dto(userID))
}

// RenameDef changes a definition's key (name) in place and rewrites its other
// fields in the same UPDATE. Immediate for the same reason the command rename
// is: the write-behind batcher coalesces by (user, name), which cannot express
// a key change. Publishes a delete for the old name and a change for the new
// so field-keyed consumers retire the stale fetch:<old> entry.
func (r *Fetches) RenameDef(ctx context.Context, userID uint64, originalName string, spec FetchSpec) error {

	originalName = normalizeName(originalName)
	spec.normalize()

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.FetchDefName(originalName); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	updated, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.FetchDefinition.Update().
			Where(
				fetchdefinition.UserIDEQ(userID),
				fetchdefinition.NameEQ(originalName),
			).
			SetName(spec.Name).
			SetURL(spec.URL).
			SetJSONPath(spec.Path).
			SetKeyLabel(spec.KeyLabel).
			SetIsActive(spec.IsActive).
			Save(ctx)
	})
	if err != nil {
		return err
	}

	// Old row absent (renamed/deleted elsewhere): fall back to a plain upsert
	// so the edit is not lost.
	if updated == 0 {
		return r.UpsertDef(ctx, userID, spec)
	}

	r.Invalidate(userID)

	r.log.Info("fetch definition renamed",
		zap.Uint64("user_id", userID),
		zap.String("from", originalName),
		zap.String("to", spec.Name),
	)

	if err := bus.PublishJSON(ctx, r.pub, data.SubjectFetchChanged, data.FetchChangedDTO{
		UserID:  userID,
		Name:    originalName,
		Deleted: true,
	}); err != nil {
		return err
	}
	return bus.PublishJSON(ctx, r.pub, data.SubjectFetchChanged, spec.dto(userID))
}

// DeleteDef hard-deletes one definition. When commands still embed its token
// the delete is refused with the referencing command names, unless forced —
// deleting silently would leave chat lines resolving a dead name. Key deletes
// are separate (DeleteKey) and always allowed; dangling labels fail closed.
func (r *Fetches) DeleteDef(ctx context.Context, userID uint64, name string, force bool) error {

	name = normalizeName(name)

	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.FetchDefName(name); err != nil {
		return err
	}

	if !force {
		referrers, err := r.ReferencingCommands(ctx, userID, name)
		if err != nil {
			return err
		}
		if len(referrers) > 0 {
			return &ErrFetchDefReferenced{Commands: referrers}
		}
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.FetchDefinition.Delete().
			Where(
				fetchdefinition.UserIDEQ(userID),
				fetchdefinition.NameEQ(name),
			).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.Invalidate(userID)

	r.log.Info("fetch definition deleted",
		zap.Uint64("user_id", userID),
		zap.String("name", name),
		zap.Bool("forced", force),
	)

	return bus.PublishJSON(ctx, r.pub, data.SubjectFetchChanged, data.FetchChangedDTO{
		UserID:  userID,
		Name:    name,
		Deleted: true,
	})
}

// ReferencingCommands scans the broadcaster's own command responses for the
// {urlfetch:<name>} token (with or without a dot-path before the closing
// brace). Chat folds the name lower-case before lookup, so the scan matches
// case-insensitively.
func (r *Fetches) ReferencingCommands(ctx context.Context, userID uint64, name string) ([]string, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().
			Where(commands.UserIDEQ(userID)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}

	var referrers []string
	for _, row := range rows {
		if responseReferencesFetch(row.Response, name) {
			referrers = append(referrers, row.Name)
		}
	}
	return referrers, nil
}

// responseReferencesFetch reports whether a command response contains the
// token {urlfetch:name} or {urlfetch:name.<path…}. The boundary check ('}' or
// '.') keeps "weather" from matching "{urlfetch:weather2}".
func responseReferencesFetch(response, name string) bool {
	haystack := strings.ToLower(response)
	needle := "{urlfetch:" + name
	for from := 0; ; {
		idx := strings.Index(haystack[from:], needle)
		if idx < 0 {
			return false
		}
		at := from + idx + len(needle)
		if at < len(response) {
			switch response[at] {
			case '}', '.':
				return true
			}
		}
		from = at
	}
}

// SetKey seals the broadcaster's API key and upserts it under the label.
// Returns the derived last4 exactly once, from the just-submitted plaintext.
// The plaintext never touches the database, logs or any reply afterwards.
func (r *Fetches) SetKey(ctx context.Context, userID uint64, label, value string) (string, error) {
	if r.packer == nil {
		return "", ErrCustodyUnavailable
	}
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	if err := validate.KeyLabel(label); err != nil {
		return "", err
	}
	if err := validate.KeyValue(value); err != nil {
		return "", err
	}

	sealed, err := r.packer.Pack([]byte(value), fetchAAD(userID, label))
	if err != nil {
		return "", err
	}
	suffix := last4(value)

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.FetchKey.Create().
			SetUserID(userID).
			SetLabel(label).
			SetKeyEnc(sealed.Ciphertext).
			SetLast4(suffix).
			OnConflictColumns(fetchkey.FieldUserID, fetchkey.FieldLabel).
			UpdateNewValues().
			Exec(ctx)
	}); err != nil {
		return "", err
	}

	r.log.Info("fetch key sealed",
		zap.Uint64("user_id", userID),
		zap.String("label", label),
	)

	return suffix, nil
}

// DeleteKey removes one labeled key — always allowed. Definitions pointing at
// it keep their dangling key_label and fail closed with "no key on file" at
// fetch time until relinked; that is the documented posture, not a bug.
func (r *Fetches) DeleteKey(ctx context.Context, userID uint64, label string) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if err := validate.KeyLabel(label); err != nil {
		return err
	}

	if err := db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.FetchKey.Delete().
			Where(
				fetchkey.UserIDEQ(userID),
				fetchkey.LabelEQ(label),
			).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.log.Info("fetch key deleted",
		zap.Uint64("user_id", userID),
		zap.String("label", label),
	)
	return nil
}

// Key unseals and returns the labeled API key, ErrNoFetchKey when none is on
// file. The plaintext serves exactly one upstream call at the caller (gossip)
// and is never cached anywhere.
func (r *Fetches) Key(ctx context.Context, userID uint64, label string) (string, error) {
	if r.packer == nil {
		return "", ErrCustodyUnavailable
	}
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.FetchKey, error) {
		return r.client.FetchKey.Query().
			Where(
				fetchkey.UserIDEQ(userID),
				fetchkey.LabelEQ(label),
			).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return "", ErrNoFetchKey
	}
	if err != nil {
		return "", err
	}

	plain, err := r.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.KeyEnc,
		AttachedData: fetchAAD(userID, label),
	})
	if err != nil {
		// AAD mismatch means corruption or tamper: terminal, logged with
		// identifiers only — never ciphertext, never plaintext.
		r.log.Error("failed to unseal fetch key",
			zap.Uint64("user_id", userID),
			zap.String("label", label),
		)
		return "", errors.New("failed to unseal fetch key")
	}
	return string(plain), nil
}

// DeleteAllForUser removes every definition and key of a deleted account.
// Idempotent — deleting absent rows succeeds silently.
func (r *Fetches) DeleteAllForUser(ctx context.Context, userID uint64) error {
	if err := db.WithExec(ctx, func(ctx context.Context) error {
		if _, err := r.client.FetchDefinition.Delete().
			Where(fetchdefinition.UserIDEQ(userID)).
			Exec(ctx); err != nil {
			return err
		}
		_, err := r.client.FetchKey.Delete().
			Where(fetchkey.UserIDEQ(userID)).
			Exec(ctx)
		return err
	}); err != nil {
		return err
	}

	r.Invalidate(userID)
	return nil
}

// CustodyEnabled reports whether the service booted with a usable Tink
// keyset; custody verbs refuse closed when it did not.
func (r *Fetches) CustodyEnabled() bool {
	return r.packer != nil
}

// Invalidate drops the cached view of one user; called when a change event
// arrives from another instance of this service.
func (r *Fetches) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(fetchesKeyPrefix, userID))
}

// Close releases the view cache.
func (r *Fetches) Close() {
	r.views.Close()
}

// defExists reports whether the user already has a row under this exact name
// (an update must not be counted against the creation quota).
func (r *Fetches) defExists(ctx context.Context, userID uint64, name string) (bool, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.client.FetchDefinition.Query().
			Where(
				fetchdefinition.UserIDEQ(userID),
				fetchdefinition.NameEQ(name),
			).
			Exist(ctx)
	})
}
