// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"os"
	"strconv"

	"ItsBagelBot/app/modules/ent"
	"ItsBagelBot/app/modules/ent/spotifycredential"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/crypto"
	"ItsBagelBot/pkg/db"
	"ItsBagelBot/pkg/env"

	"go.uber.org/zap"
)

// ErrNoSpotifyToken marks a broadcaster with no connected Spotify account.
var ErrNoSpotifyToken = errors.New("no spotify refresh token on record")

// SpotifyCreds is the custody store for broadcaster Spotify OAuth refresh
// tokens, sealed at rest with the modules service's own AEAD keyset — the
// spotify twin of GoveeCreds. It shares the service's ent client but is its
// own type so the general module store stays free of any crypto dependency:
// only this narrow surface touches plaintext tokens, and it never caches or
// logs them.
type SpotifyCreds struct {
	client *ent.Client
	packer domaincrypto.Packer
}

// NewSpotifyCreds builds the token store over the shared ent client.
func NewSpotifyCreds(client *ent.Client, packer domaincrypto.Packer) *SpotifyCreds {
	return &SpotifyCreds{client: client, packer: packer}
}

// NewSpotifyCredsFromEnv builds the token store from the service's Tink
// keyset (TINK_KEYSET_PATH) — the SAME keyset and the same best-effort rules
// as NewGoveeCredsFromEnv, since both custody stores seal under one keyset.
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

// SetToken seals the broadcaster's refresh token and upserts it. The
// plaintext never touches the database or logs; the AAD binds the ciphertext
// to this user id so an envelope copied onto another row fails to open.
func (s *SpotifyCreds) SetToken(ctx context.Context, userID uint64, refreshToken string) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	if refreshToken == "" {
		return errors.New("empty spotify refresh token")
	}

	sealed, err := s.packer.Pack([]byte(refreshToken), spotifyAAD(userID))
	if err != nil {
		return err
	}

	return db.WithExec(ctx, func(ctx context.Context) error {
		return s.client.SpotifyCredential.Create().
			SetUserID(userID).
			SetTokenEnc(sealed.Ciphertext).
			OnConflictColumns(spotifycredential.FieldUserID).
			UpdateNewValues().
			Exec(ctx)
	})
}

// ClearToken removes the broadcaster's stored refresh token ("disconnect"). A
// missing row is a no-op: the end state (no token) is the same either way.
func (s *SpotifyCreds) ClearToken(ctx context.Context, userID uint64) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := s.client.SpotifyCredential.Delete().
			Where(spotifycredential.UserIDEQ(userID)).
			Exec(ctx)
		return err
	})
}

// tokenRow loads the (validated) broadcaster's sealed credential row, mapping
// a missing row to ErrNoSpotifyToken. Both HasToken and Token read through it.
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

// HasToken reports whether the broadcaster has a Spotify connection on file —
// the status the dashboard shows ("connected"), never the value.
func (s *SpotifyCreds) HasToken(ctx context.Context, userID uint64) (bool, error) {
	_, err := s.tokenRow(ctx, userID)
	switch {
	case errors.Is(err, ErrNoSpotifyToken):
		return false, nil
	case err != nil:
		return false, err
	default:
		return true, nil
	}
}

// Token unseals and returns the stored OAuth refresh token. Returns
// ErrNoSpotifyToken when the broadcaster has none. The plaintext is returned
// to the caller (gossip) and deliberately never cached — gossip derives a
// short-lived access token from it per call window and holds nothing else.
func (s *SpotifyCreds) Token(ctx context.Context, userID uint64) (string, error) {
	row, err := s.tokenRow(ctx, userID)
	if err != nil {
		return "", err
	}
	plain, err := s.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   row.TokenEnc,
		AttachedData: spotifyAAD(userID),
	})
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func spotifyAAD(userID uint64) []byte {
	aad := make([]byte, 0, 20+len("|spotify_token"))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|spotify_token"...)
	return aad
}
