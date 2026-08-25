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

// ErrNoSpotifyApp marks a broadcaster who has not registered their own Spotify
// application yet. It is distinct from ErrNoSpotifyToken on purpose: the
// console has to tell "paste your app credentials" apart from "now connect
// your account", and after the fleet-wide app was retired the first step is
// the one broadcasters land on.
var ErrNoSpotifyApp = errors.New("no spotify application on record")

// SpotifyApp is a broadcaster's own registered Spotify application: the pair
// that authenticates every exchange against accounts.spotify.com. It travels
// as one value because half of it authenticates nothing, and because a bare
// (string, string) pair is exactly the shape a caller eventually swaps by
// accident.
type SpotifyApp struct {
	ClientID     string
	ClientSecret string
}

// SpotifySetup is everything one broadcaster has on file: the application they
// registered and the grant minted against it. RefreshToken is empty for a
// broadcaster who pasted credentials but never finished the connect flow —
// an ordinary state, not a failure.
type SpotifySetup struct {
	App          SpotifyApp
	RefreshToken string
}

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

	sealed, err := s.packer.Pack([]byte(refreshToken), spotifyAAD(userID, fieldToken))
	if err != nil {
		return err
	}

	// The conflict update names its columns explicitly rather than using
	// UpdateNewValues: that helper writes back every column the INSERT
	// carries, defaults included, so it would blank the client_id/
	// client_secret_enc pair (both defaulted, neither set here) and silently
	// unregister the broadcaster's Spotify app on every reconnect.
	return db.WithExec(ctx, func(ctx context.Context) error {
		return s.client.SpotifyCredential.Create().
			SetUserID(userID).
			SetTokenEnc(sealed.Ciphertext).
			OnConflictColumns(spotifycredential.FieldUserID).
			Update(func(u *ent.SpotifyCredentialUpsert) {
				u.SetTokenEnc(sealed.Ciphertext).SetUpdatedAt(time.Now())
			}).
			Exec(ctx)
	})
}

// SetApp seals the broadcaster's own Spotify application secret and upserts it
// alongside the (public) client id. Replacing the application deliberately
// leaves any existing refresh token in place: a re-paste of the same app's
// credentials must not force a reconnect, and a token minted by a DIFFERENT
// app fails at the next exchange, which surfaces as the ordinary "connect
// again" path rather than as a silent wipe here.
func (s *SpotifyCreds) SetApp(ctx context.Context, userID uint64, app SpotifyApp) error {
	if err := validate.UserID(userID); err != nil {
		return err
	}
	clientID := strings.TrimSpace(app.ClientID)
	clientSecret := strings.TrimSpace(app.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return errors.New("spotify client id and secret are both required")
	}

	sealed, err := s.packer.Pack([]byte(clientSecret), spotifyAAD(userID, fieldApp))
	if err != nil {
		return err
	}

	// Explicit conflict columns for the same reason as SetToken, mirrored: an
	// UpdateNewValues here would blank the token_enc of a broadcaster who
	// rotates their client secret, forcing a reconnect nobody asked for.
	return db.WithExec(ctx, func(ctx context.Context) error {
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

// ClearApp drops the stored application. The refresh token goes with it: a
// grant is minted BY an application, so a token outliving its app can only
// produce confusing 400s at the next exchange.
func (s *SpotifyCreds) ClearApp(ctx context.Context, userID uint64) error {
	return s.ClearToken(ctx, userID)
}

// AppClientID reports whether the broadcaster has pasted their Spotify
// application credentials — the first of the two console steps — by returning
// the client id, or "" when they have not.
//
// It deliberately returns the id rather than a SpotifyApp: this backs the
// console's status verb, and a shape that cannot carry the client secret
// cannot leak it onto a dashboard-facing subject either.
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

// Credentials returns everything gossip needs to authenticate one broadcaster
// in a single read: their application plus the grant minted against it. A
// broadcaster with no application gets ErrNoSpotifyApp — there is nothing to
// authenticate against — while an application with no grant yet comes back
// with an empty refresh token, which the caller reports as "not connected".
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

// tokenFromRow unseals the refresh token off an already-loaded row. The row
// carries its own owner, so the AAD is derived from it rather than from an id
// passed alongside — one fewer way to unseal against the wrong user. A row
// that carries an application but no grant yet (credentials pasted, connect
// flow unfinished) is ErrNoSpotifyToken, not a bad seal.
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

// appFromRow unseals the application off an already-loaded row — same
// owner-from-the-row rule as tokenFromRow — mapping a row that carries no
// application to ErrNoSpotifyApp.
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

// ErrRotateStale marks a rotation whose prev token no longer matches the
// stored one — another writer got there first (a concurrent mint's rotation,
// or a fresh console reconnect). The store keeps the newer value.
var ErrRotateStale = errors.New("spotify refresh token changed since this rotation was minted")

// RotateToken compare-and-swaps the broadcaster's stored refresh token after
// Spotify rotated it on exchange: the new token is stored only while prev
// still matches what is on file. The match runs on the unsealed value — AEAD
// ciphertexts are non-deterministic, so comparing envelopes in SQL cannot
// work, which is also why this is read-compare-write rather than a
// conditional UPDATE. The unguarded window between read and upsert is
// accepted: rotations are rare and per-broadcaster, and a loser in that
// window merely re-stores a token Spotify just issued.
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
	return s.SetToken(ctx, userID, next)
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
	row, err := s.tokenRow(ctx, userID)
	switch {
	case errors.Is(err, ErrNoSpotifyToken):
		return false, nil
	case err != nil:
		return false, err
	default:
		return len(row.TokenEnc) > 0, nil
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
	return s.tokenFromRow(row)
}

// spotifyField names which of the row's two sealed columns an envelope
// belongs to. It is a type rather than a bare string so the two labels cannot
// be passed interchangeably, which is the whole point of them differing.
type spotifyField string

const (
	fieldToken spotifyField = "|spotify_token"
	fieldApp   spotifyField = "|spotify_app"
)

// spotifyAAD binds a ciphertext to its owner AND to the column it lives in:
// the field label is part of the associated data, so an envelope copied
// between token_enc and client_secret_enc — or onto another user's row — fails
// to open.
func spotifyAAD(userID uint64, field spotifyField) []byte {
	aad := make([]byte, 0, 20+len(field))
	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, field...)
	return aad
}
