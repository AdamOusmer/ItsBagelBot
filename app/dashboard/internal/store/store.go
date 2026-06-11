// Package store owns the dashboard schema: users, sessions (managed by scs),
// and encrypted bot grants. This service is the only one allowed to touch the
// `dashboard` schema; everything else reaches it over NATS.
package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Store struct {
	DB *sql.DB
}

type User struct {
	TwitchUserID string
	Login        string
	DisplayName  string
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

// Migrate creates the schema. Idempotent; runs at every boot.
func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			token  CHAR(43) PRIMARY KEY,
			data   BLOB NOT NULL,
			expiry TIMESTAMP(6) NOT NULL
		)`,
		`CREATE INDEX sessions_expiry_idx ON sessions (expiry)`,
		`CREATE TABLE IF NOT EXISTS users (
			twitch_user_id VARCHAR(32) PRIMARY KEY,
			login          VARCHAR(64)  NOT NULL,
			display_name   VARCHAR(128) NOT NULL,
			created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS bot_grants (
			broadcaster_user_id VARCHAR(32) PRIMARY KEY,
			scopes              VARCHAR(512) NOT NULL,
			refresh_token_enc   VARBINARY(2048) NOT NULL,
			created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		)`,
	}
	for _, q := range stmts {
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			// the bare CREATE INDEX has no IF NOT EXISTS in MySQL; 1061 = duplicate key name
			if isDuplicateIndex(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func isDuplicateIndex(err error) bool {
	type causer interface{ Error() string }
	if e, ok := err.(causer); ok {
		return len(e.Error()) > 0 && (contains(e.Error(), "Duplicate key name") || contains(e.Error(), "1061"))
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}

func (s *Store) UpsertUser(ctx context.Context, u User) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO users (twitch_user_id, login, display_name) VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE login = VALUES(login), display_name = VALUES(display_name)`,
		u.TwitchUserID, u.Login, u.DisplayName)
	return err
}

func (s *Store) SaveBotGrant(ctx context.Context, broadcasterID, scopes string, refreshTokenEnc []byte) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO bot_grants (broadcaster_user_id, scopes, refresh_token_enc) VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE scopes = VALUES(scopes), refresh_token_enc = VALUES(refresh_token_enc)`,
		broadcasterID, scopes, refreshTokenEnc)
	return err
}

func (s *Store) HasBotGrant(ctx context.Context, broadcasterID string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bot_grants WHERE broadcaster_user_id = ?`, broadcasterID).Scan(&n)
	return n > 0, err
}
