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

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/spotifycredential"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"

	"go.uber.org/zap"
)

var ErrNoSpotifyToken = errors.New("no spotify refresh token on record")

var ErrNoSpotifyApp = errors.New("no spotify application on record")

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
	Present bool
	Scopes  []string
}

type SpotifyCreds struct {
	client *ent.Client
	packer domaincrypto.Packer
}

func NewSpotifyCreds(client *ent.Client, packer domaincrypto.Packer) *SpotifyCreds {
	return &SpotifyCreds{client: client, packer: packer}
}

func NewSpotifyCredsFromEnv(client *ent.Client, log *zap.Logger) *SpotifyCreds {
	path := env.Get("TINK_KEYSET_PATH", "")
	if path == "" {
		log.Warn("spotify key custody disabled: TINK_KEYSET_PATH not set")
		return nil
	}
	keysetJSON, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("spotify key custody disabled: keyset not provisioned yet", zap.String("path", path))
		return nil
	}
	if err != nil {
		log.Fatal("failed to read tink keyset", zap.Error(err))
	}
	packer, err := crypto.NewCrypto(keysetJSON)
	if err != nil {
		log.Fatal("failed to initialize crypto", zap.Error(err))
	}
	return NewSpotifyCreds(client, packer)
}

func (s *SpotifyCreds) write(ctx context.Context, userID uint64, stmt func(context.Context) error) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	return db.WithExec(ctx, stmt)
}

func (s *SpotifyCreds) SetToken(ctx context.Context, userID uint64, grant SpotifyGrant) error {
	if grant.RefreshToken == "" {
		return errors.New("empty spotify refresh token")
	}
	sealed, err := s.packer.Pack([]byte(grant.RefreshToken), spotifyAAD(userID, fieldToken))
	if err != nil {
		return err
	}

	// Must name columns, not UpdateNewValues, which would blank the stored app on every reconnect.
	joined := strings.Join(grant.Scopes, " ")
	return s.write(ctx, userID, func(ctx context.Context) error {
		create := s.client.SpotifyCredential.Create().
			SetUserID(userID).
			SetTokenEnc(sealed.Ciphertext)
		if grant.Scopes != nil {
			create = create.SetScopes(joined)
		}
		return create.
			OnConflictColumns(spotifycredential.FieldUserID).
			Update(func(u *ent.SpotifyCredentialUpsert) {
				u.SetTokenEnc(sealed.Ciphertext).SetUpdatedAt(time.Now())
				if grant.Scopes != nil {
					u.SetScopes(joined)
				}
			}).
			Exec(ctx)
	})
}

func (s *SpotifyCreds) SetApp(ctx context.Context, userID uint64, app SpotifyApp) error {
	clientID := strings.TrimSpace(app.ClientID)
	clientSecret := strings.TrimSpace(app.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return errors.New("spotify client id and secret are both required")
	}

	sealed, err := s.packer.Pack([]byte(clientSecret), spotifyAAD(userID, fieldApp))
	if err != nil {
		return err
	}

	// Must name columns, not UpdateNewValues, which would blank token_enc.
	return s.write(ctx, userID, func(ctx context.Context) error {
		return s.client.SpotifyCredential.Create().
			SetUserID(userID).
			SetClientID(clientID).
			SetClientSecretEnc(sealed.Ciphertext).
			OnConflictColumns(spotifycredential.FieldUserID).
			Update(func(u *ent.SpotifyCredentialUpsert) {
				u.SetClientID(clientID).SetClientSecretEnc(sealed.Ciphertext).SetUpdatedAt(time.Now())
			}).
			Exec(ctx)
	})
}

func (s *SpotifyCreds) ClearApp(ctx context.Context, userID uint64) error {
	return s.write(ctx, userID, func(ctx context.Context) error {
		_, err := s.client.SpotifyCredential.Delete().
			Where(spotifycredential.UserIDEQ(userID)).
			Exec(ctx)
		return err
	})
}

// Must not return the secret: it backs a dashboard-facing subject.
func (s *SpotifyCreds) AppClientID(ctx context.Context, userID uint64) (string, error) {
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

func (s *SpotifyCreds) Credentials(ctx context.Context, userID uint64) (SpotifySetup, error) {
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
		AttachedData: spotifyAAD(row.UserID, fieldToken),
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
		AttachedData: spotifyAAD(row.UserID, fieldApp),
	})
	if err != nil {
		return SpotifyApp{}, err
	}
	return SpotifyApp{ClientID: row.ClientID, ClientSecret: string(plain)}, nil
}

func (s *SpotifyCreds) ClearToken(ctx context.Context, userID uint64) error {
	return s.write(ctx, userID, func(ctx context.Context) error {
		_, err := s.client.SpotifyCredential.Update().
			Where(spotifycredential.UserIDEQ(userID)).
			ClearTokenEnc().
			Save(ctx)
		return err
	})
}

var ErrRotateStale = errors.New("spotify refresh token changed since this rotation was minted")

func (s *SpotifyCreds) RotateToken(ctx context.Context, userID uint64, prev, next string) error {
	if next == "" {
		return errors.New("empty spotify refresh token")
	}
	current, err := s.Token(ctx, userID)
	if err != nil {
		return err
	}
	if current != prev {
		return ErrRotateStale
	}
	return s.SetToken(ctx, userID, SpotifyGrant{RefreshToken: next})
}

func (s *SpotifyCreds) tokenRow(ctx context.Context, userID uint64) (*ent.SpotifyCredential, error) {
	if err := validate.UserID(userID); err != nil {
		return nil, err
	}
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.SpotifyCredential, error) {
		return s.client.SpotifyCredential.Query().
			Where(spotifycredential.UserIDEQ(userID)).
			Only(ctx)
	})
	if ent.IsNotFound(err) {
		return nil, ErrNoSpotifyToken
	}
	return row, err
}

func (s *SpotifyCreds) HasToken(ctx context.Context, userID uint64) (bool, error) {
	status, err := s.TokenStatus(ctx, userID)
	return status.Present, err
}

func (s *SpotifyCreds) TokenStatus(ctx context.Context, userID uint64) (SpotifyGrantStatus, error) {
	row, err := s.tokenRow(ctx, userID)
	switch {
	case errors.Is(err, ErrNoSpotifyToken):
		return SpotifyGrantStatus{}, nil
	case err != nil:
		return SpotifyGrantStatus{}, err
	case len(row.TokenEnc) == 0:
		return SpotifyGrantStatus{}, nil
	default:
		return SpotifyGrantStatus{Present: true, Scopes: strings.Fields(row.Scopes)}, nil
	}
}

func (s *SpotifyCreds) Token(ctx context.Context, userID uint64) (string, error) {
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

func spotifyAAD(userID uint64, field spotifyField) []byte {
	aad := make([]byte, 0, 20+len(field))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, field...)
	return aad
}
