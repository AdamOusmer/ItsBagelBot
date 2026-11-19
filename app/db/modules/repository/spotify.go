// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"

	"go.uber.org/zap"
)

var ErrNoSpotifyToken = errors.New("no spotify refresh token on record")

var ErrNoSpotifyApp = errors.New("no spotify application on record")

type SpotifyUserID uint64

type SpotifyApp struct {
	ClientID     string
	ClientSecret string
}

type SpotifySetup struct {
	App          SpotifyApp
	RefreshToken string
}

type SpotifyGrant struct {
	RefreshToken string
	Scopes       []string
}

type SpotifyGrantStatus struct {
	Present        bool
	Scopes         []string
	NeedsReconnect bool
}

type SpotifyCreds struct {
	client        *ent.Client
	packer        domaincrypto.Packer
	invalidations *bus.Invalidations
}

func NewSpotifyCreds(client *ent.Client, packer domaincrypto.Packer, invalidations *bus.Invalidations) *SpotifyCreds {
	return &SpotifyCreds{client: client, packer: packer, invalidations: invalidations}
}

func NewSpotifyCredsFromEnv(client *ent.Client, log *zap.Logger, invalidations *bus.Invalidations) *SpotifyCreds {
	packer := crypto.OptionalFromEnv("spotify", log)
	if packer == nil {
		return nil
	}
	return NewSpotifyCreds(client, packer, invalidations)
}

func (s *SpotifyCreds) write(ctx context.Context, userID SpotifyUserID, stmt func(context.Context) error) error {
	if err := validate.UserID(uint64(userID)); err != nil {
		return err
	}
	return db.WithExec(ctx, stmt)
}

func (s *SpotifyCreds) changed(userID SpotifyUserID, affected bool, err error) error {
	if err == nil && affected {
		s.invalidations.Publish(invalidate.ScopeSpotify, uint64(userID))
	}
	return err
}

func (s *SpotifyCreds) SetToken(ctx context.Context, userID SpotifyUserID, grant SpotifyGrant) error {
	if grant.RefreshToken == "" {
		return errors.New("empty spotify refresh token")
	}
	changed, err := s.tokenChanges(ctx, userID, grant)
	if err != nil {
		return err
	}
	sealed, err := s.packer.Pack([]byte(grant.RefreshToken), spotifyAAD(userID, fieldToken))
	if err != nil {
		return err
	}

	// Must name columns, not UpdateNewValues, which would blank the stored app on every reconnect.
	joined := strings.Join(grant.Scopes, " ")
	err = s.write(ctx, userID, func(ctx context.Context) error {
		create := s.client.SpotifyCredential.Create().
			SetUserID(uint64(userID)).
			SetTokenEnc(sealed.Ciphertext)
		if grant.Scopes != nil {
			create = create.SetScopes(joined)
		}
		return create.
			OnConflictColumns(spotifycredential.FieldUserID).
			Update(func(u *ent.SpotifyCredentialUpsert) {
				u.SetTokenEnc(sealed.Ciphertext).ClearRefreshFailedAt().SetUpdatedAt(time.Now())
				if grant.Scopes != nil {
					u.SetScopes(joined)
				}
			}).
			Exec(ctx)
	})
	return s.changed(userID, changed, err)
}

func (s *SpotifyCreds) SetApp(ctx context.Context, userID SpotifyUserID, app SpotifyApp) error {
	clientID := strings.TrimSpace(app.ClientID)
	clientSecret := strings.TrimSpace(app.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return errors.New("spotify client id and secret are both required")
	}
	changed, err := s.appChanges(ctx, userID, SpotifyApp{ClientID: clientID, ClientSecret: clientSecret})
	if err != nil {
		return err
	}

	sealed, err := s.packer.Pack([]byte(clientSecret), spotifyAAD(userID, fieldApp))
	if err != nil {
		return err
	}

	// Must name columns, not UpdateNewValues, which would blank token_enc.
	err = s.write(ctx, userID, func(ctx context.Context) error {
		return s.client.SpotifyCredential.Create().
			SetUserID(uint64(userID)).
			SetClientID(clientID).
			SetClientSecretEnc(sealed.Ciphertext).
			OnConflictColumns(spotifycredential.FieldUserID).
			Update(func(u *ent.SpotifyCredentialUpsert) {
				u.SetClientID(clientID).SetClientSecretEnc(sealed.Ciphertext).SetUpdatedAt(time.Now())
			}).
			Exec(ctx)
	})
	return s.changed(userID, changed, err)
}

func (s *SpotifyCreds) ClearApp(ctx context.Context, userID SpotifyUserID) error {
	var affected int
	err := s.write(ctx, userID, func(ctx context.Context) error {
		n, err := s.client.SpotifyCredential.Delete().
			Where(spotifycredential.UserIDEQ(uint64(userID))).
			Exec(ctx)
		affected = n
		return err
	})
	return s.changed(userID, affected > 0, err)
}

func (s *SpotifyCreds) AppClientID(ctx context.Context, userID SpotifyUserID) (string, error) {
	row, err := s.tokenRow(ctx, userID)
	if errors.Is(err, ErrNoSpotifyToken) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(row.ClientSecretEnc) == 0 {
		return "", nil
	}
	return row.ClientID, nil
}

func (s *SpotifyCreds) Credentials(ctx context.Context, userID SpotifyUserID) (SpotifySetup, error) {
	row, err := s.tokenRow(ctx, userID)
	if errors.Is(err, ErrNoSpotifyToken) {
		return SpotifySetup{}, ErrNoSpotifyApp
	}
	if err != nil {
		return SpotifySetup{}, err
	}
	app, err := s.appFromRow(row)
	if err != nil {
		return SpotifySetup{}, err
	}
	token, err := s.tokenFromRow(row)
	if errors.Is(err, ErrNoSpotifyToken) {
		return SpotifySetup{App: app}, nil
	}
	if err != nil {
		return SpotifySetup{}, err
	}
	return SpotifySetup{App: app, RefreshToken: token}, nil
}

func (s *SpotifyCreds) tokenFromRow(row *ent.SpotifyCredential) (string, error) {
	if len(row.TokenEnc) == 0 {
		return "", ErrNoSpotifyToken
	}
	plain, err := s.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.TokenEnc,
		AttachedData: spotifyAAD(SpotifyUserID(row.UserID), fieldToken),
	})
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *SpotifyCreds) appFromRow(row *ent.SpotifyCredential) (SpotifyApp, error) {
	if row.ClientID == "" || len(row.ClientSecretEnc) == 0 {
		return SpotifyApp{}, ErrNoSpotifyApp
	}
	plain, err := s.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.ClientSecretEnc,
		AttachedData: spotifyAAD(SpotifyUserID(row.UserID), fieldApp),
	})
	if err != nil {
		return SpotifyApp{}, err
	}
	return SpotifyApp{ClientID: row.ClientID, ClientSecret: string(plain)}, nil
}

func (s *SpotifyCreds) ClearToken(ctx context.Context, userID SpotifyUserID) error {
	return s.updateGrant(ctx, userID, (*ent.SpotifyCredentialUpdate).ClearTokenEnc, (*ent.SpotifyCredentialUpdate).ClearRefreshFailedAt)
}

func (s *SpotifyCreds) MarkTokenDead(ctx context.Context, userID SpotifyUserID, token string) error {
	row, err := s.tokenRow(ctx, userID)
	if errors.Is(err, ErrNoSpotifyToken) {
		return nil
	}
	if err != nil {
		return err
	}
	current, err := s.tokenFromRow(row)
	if errors.Is(err, ErrNoSpotifyToken) {
		return nil
	}
	if err != nil {
		return err
	}
	if current != token || row.RefreshFailedAt != nil {
		return nil
	}
	affected, err := s.updateSealed(ctx, userID, row.TokenEnc, func(u *ent.SpotifyCredentialUpdate) *ent.SpotifyCredentialUpdate {
		return u.Where(spotifycredential.RefreshFailedAtIsNil()).SetRefreshFailedAt(time.Now())
	})
	return s.changed(userID, affected > 0, err)
}

func (s *SpotifyCreds) updateGrant(ctx context.Context, userID SpotifyUserID, edits ...func(*ent.SpotifyCredentialUpdate) *ent.SpotifyCredentialUpdate) error {
	var affected int
	err := s.write(ctx, userID, func(ctx context.Context) error {
		update := s.client.SpotifyCredential.Update().Where(spotifycredential.UserIDEQ(uint64(userID)), spotifycredential.Or(spotifycredential.TokenEncNotNil(), spotifycredential.RefreshFailedAtNotNil()))
		for _, edit := range edits {
			update = edit(update)
		}
		n, err := update.Save(ctx)
		affected = n
		return err
	})
	return s.changed(userID, affected > 0, err)
}

func (s *SpotifyCreds) updateSealed(ctx context.Context, userID SpotifyUserID, sealed []byte, edit func(*ent.SpotifyCredentialUpdate) *ent.SpotifyCredentialUpdate) (int, error) {
	var affected int
	err := s.write(ctx, userID, func(ctx context.Context) error {
		n, err := edit(s.client.SpotifyCredential.Update().
			Where(spotifycredential.UserIDEQ(uint64(userID)), spotifycredential.TokenEncEQ(sealed))).
			Save(ctx)
		affected = n
		return err
	})
	return affected, err
}

var ErrRotateStale = errors.New("spotify refresh token changed since this rotation was minted")

func (s *SpotifyCreds) RotateToken(ctx context.Context, userID SpotifyUserID, prev, next string) error {
	if next == "" {
		return errors.New("empty spotify refresh token")
	}
	row, err := s.tokenRow(ctx, userID)
	if err != nil {
		return err
	}
	current, err := s.tokenFromRow(row)
	if err != nil {
		return err
	}
	if current != prev {
		return ErrRotateStale
	}
	sealed, err := s.packer.Pack([]byte(next), spotifyAAD(userID, fieldToken))
	if err != nil {
		return err
	}
	affected, err := s.updateSealed(ctx, userID, row.TokenEnc, func(u *ent.SpotifyCredentialUpdate) *ent.SpotifyCredentialUpdate {
		return u.SetTokenEnc(sealed.Ciphertext).ClearRefreshFailedAt().SetUpdatedAt(time.Now())
	})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrRotateStale
	}
	return nil
}

func (s *SpotifyCreds) tokenRow(ctx context.Context, userID SpotifyUserID) (*ent.SpotifyCredential, error) {
	if err := validate.UserID(uint64(userID)); err != nil {
		return nil, err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.SpotifyCredential, error) {
		return s.client.SpotifyCredential.Query().
			Where(spotifycredential.UserIDEQ(uint64(userID))).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrNoSpotifyToken
	}
	return row, err
}

func (s *SpotifyCreds) HasToken(ctx context.Context, userID SpotifyUserID) (bool, error) {
	status, err := s.TokenStatus(ctx, userID)
	return status.Present, err
}

func (s *SpotifyCreds) TokenStatus(ctx context.Context, userID SpotifyUserID) (SpotifyGrantStatus, error) {
	row, err := s.tokenRow(ctx, userID)
	switch {
	case errors.Is(err, ErrNoSpotifyToken):
		return SpotifyGrantStatus{}, nil
	case err != nil:
		return SpotifyGrantStatus{}, err
	case len(row.TokenEnc) == 0:
		return SpotifyGrantStatus{}, nil
	default:
		return SpotifyGrantStatus{
			Present:        true,
			Scopes:         strings.Fields(row.Scopes),
			NeedsReconnect: row.RefreshFailedAt != nil,
		}, nil
	}
}

func (s *SpotifyCreds) Token(ctx context.Context, userID SpotifyUserID) (string, error) {
	row, err := s.tokenRow(ctx, userID)
	if err != nil {
		return "", err
	}
	return s.tokenFromRow(row)
}

type spotifyField string

const (
	fieldToken spotifyField = "|spotify_token"
	fieldApp   spotifyField = "|spotify_app"
)

func spotifyAAD(userID SpotifyUserID, field spotifyField) []byte {
	aad := make([]byte, 0, 20+len(field))
	aad = strconv.AppendUint(aad, uint64(userID), 10)
	aad = append(aad, field...)
	return aad
}
