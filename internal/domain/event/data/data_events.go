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
	UserID             uint64 `json:"user_id"`
	Username           string `json:"username"`
	IsActive           bool   `json:"is_active"`
	Status             string `json:"status"`
	Banned             bool   `json:"banned"`
	Locale             string `json:"locale,omitempty"`
	CommandsPageHidden bool   `json:"commands_page_hidden"`
}

type UserDeletedDTO struct {
	UserID uint64 `json:"user_id"`
}

type ModuleChangedDTO struct {
	UserID    uint64           `json:"user_id"`
	Name      string           `json:"name"`
	IsEnabled bool             `json:"is_enabled"`
	Configs   codec.RawMessage `json:"configs,omitempty"`
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
	Uses             uint64   `json:"uses,omitempty"`
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
	UserID uint64 `json:"user_id"`
	Name   string `json:"name"`
	Count  uint64 `json:"count,omitempty"`
}
