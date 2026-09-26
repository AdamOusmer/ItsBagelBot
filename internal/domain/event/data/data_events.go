// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package data

import "ItsBagelBot/pkg/codec"

const (
	SubjectUserChanged    = "data.users.changed"
	SubjectUserDeleted    = "data.users.deleted"
	SubjectModuleChanged  = "data.modules.changed"
	SubjectCommandChanged = "data.commands.changed"
	SubjectCommandUsed    = "data.commands.used"

	SubjectFetchChanged = "data.commands.fetch_changed"

	SubjectReprojectRequest = "data.reproject.request"
)

type UserChangedDTO struct {
	StateRevision int64  `json:"state_revision,omitempty"`
	UserID        uint64 `json:"user_id"`
	// AccountCreatedAt identifies this incarnation of a broadcaster account.
	// A deleted Twitch ID may register again; old events must not restore or
	// remove the newer account. Unix microseconds stay exact in Valkey Lua.
	AccountCreatedAt int64  `json:"account_created_at,omitempty"`
	Username         string `json:"username"`
	IsActive         bool   `json:"is_active"`
	Status           string `json:"status"`
	Banned           bool   `json:"banned"`
	// Locale is the user's console UI language, projected so the worker can
	// answer system commands in their language. Omitted by older publishers;
	// the projector treats an empty value as "unchanged" and never clobbers a
	// previously projected locale.
	Locale string `json:"locale,omitempty"`
	// CommandsPageHidden mirrors the inverted flag (D2): a publisher that lacks
	// this field zero-values to false, so a projector still on the old shape
	// folds "visible", the pre-feature behaviour.
	CommandsPageHidden bool `json:"commands_page_hidden"`
}

type UserDeletedDTO struct {
	UserID           uint64 `json:"user_id"`
	AccountCreatedAt int64  `json:"account_created_at,omitempty"`
}

type ModuleChangedDTO struct {
	AccountCreatedAt int64            `json:"account_created_at,omitempty"`
	UserID           uint64           `json:"user_id"`
	Name             string           `json:"name"`
	IsEnabled        bool             `json:"is_enabled"`
	Configs          codec.RawMessage `json:"configs,omitempty"`
	Revision         int              `json:"revision,omitempty"`
}

type CommandChangedDTO struct {
	UserID           uint64   `json:"user_id"`
	Name             string   `json:"name"`
	Aliases          []string `json:"aliases,omitempty"`
	Response         string   `json:"response,omitempty"`
	IsActive         bool     `json:"is_active"`
	StreamOnlineOnly bool     `json:"stream_online_only"`
	Perm             string   `json:"perm,omitempty"`
	Cooldown         uint     `json:"cooldown,omitempty"`
	AllowedUserID    uint64   `json:"allowed_user_id,omitempty"`
	Uses             int64    `json:"uses,omitempty,string"`
	BumpCounter      string   `json:"bump_counter,omitempty"`
	Deleted          bool     `json:"deleted"`
}

type FetchChangedDTO struct {
	UserID   uint64   `json:"user_id"`
	Name     string   `json:"name"`
	URL      string   `json:"url,omitempty"`
	JSONPath []string `json:"json_path,omitempty"`
	KeyLabel string   `json:"key_label,omitempty"`
	IsActive bool     `json:"is_active"`
	Deleted  bool     `json:"deleted"`
}

type CommandUsedDTO struct {
	BatchID string `json:"batch_id,omitempty"`
	UserID  uint64 `json:"user_id"`
	Name    string `json:"name"`
	Count   int64  `json:"count,omitempty"`
}
