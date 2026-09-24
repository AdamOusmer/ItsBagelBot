// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/commands"
	"ItsBagelBot/app/db/commands/ent/fetchdefinition"
	"ItsBagelBot/app/db/commands/ent/fetchkey"
	"ItsBagelBot/internal/domain/event/data"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"

	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/pkg/tmpl"

	"go.uber.org/zap"
)

var ErrNoFetchKey = errors.New("no fetch key on record")

var ErrCustodyUnavailable = errors.New("key custody unavailable")

type ErrFetchDefReferenced struct {
	Commands []string
}

func (e *ErrFetchDefReferenced) Error() string {
	return "still referenced by commands: " + strings.Join(e.Commands, ", ")
}

const (
	fetchesKeyPrefix = "fetchdefs:"

	fetchesCacheTTL = 5 * time.Minute

	fetchesCacheCapacity int64 = 4096
)

type FetchView = fetchkeyrpc.FetchView

type KeyView = fetchkeyrpc.KeyView

type Fetches struct {
	client *ent.Client
	packer domaincrypto.Packer
	views  *cache.Cache[[]FetchView]
	pub    bus.Publisher
	log    *zap.Logger
}

func NewFetches(client *ent.Client, packer domaincrypto.Packer, pub bus.Publisher, log *zap.Logger) *Fetches {
	return &Fetches{
		client: client,
		packer: packer,
		views:  cache.New[[]FetchView](fetchesCacheCapacity, fetchesCacheTTL),
		pub:    pub,
		log:    log,
	}
}

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

	// Never log the URL query or any key material.
	r.log.Info("fetch definition saved",
		zap.Uint64("user_id", userID),
		zap.String("name", spec.Name),
	)

	return bus.PublishJSON(ctx, r.pub, data.SubjectFetchChanged, spec.dto(userID))
}

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

type DefDelete struct {
	Name  string
	Force bool
}

func (r *Fetches) DeleteDef(ctx context.Context, userID uint64, del DefDelete) error {
	name, force := del.Name, del.Force

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

const urlFetchTokenName = "urlfetch"

func (r *Fetches) ReferencingCommands(ctx context.Context, userID uint64, name string) ([]string, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.Commands, error) {
		return r.client.Commands.Query().
			Where(commands.UserIDEQ(userID)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}

	want := tmpl.NormalizeName(name)
	if want == "" {
		return nil, nil
	}
	var referrers []string
	for _, row := range rows {
		if referencesFetch(row.Response, want) {
			referrers = append(referrers, row.Name)
		}
	}
	return referrers, nil
}

func referencesFetch(response, want string) bool {
	for _, tok := range tmpl.Lex(response) {
		if namesFetchDef(tok, want) {
			return true
		}
	}
	return false
}

func namesFetchDef(tok tmpl.Token, want string) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == urlFetchTokenName && fetchDefName(tok) == want
}

func fetchDefName(tok tmpl.Token) string {
	if !tok.HasPayload {
		return ""
	}
	def, _, _ := strings.Cut(tmpl.NormalizeName(tok.Payload), ".")
	return def
}

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

func (r *Fetches) CustodyEnabled() bool {
	return r.packer != nil
}

func (r *Fetches) Invalidate(userID uint64) {
	r.views.Invalidate(cache.UserKey(fetchesKeyPrefix, userID))
}

func (r *Fetches) Close() {
	r.views.Close()
}

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
