// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc/discorddata"
)

type moduleRPC struct{ repo ModuleStore }

func subscribeModules(w Wiring) error {
	h := moduleRPC{repo: w.Repo}
	return errors.Join(
		serve(w, discorddata.VerbModuleList, h.list),
		serve(w, discorddata.VerbModuleSet, h.set),
	)
}

func (h moduleRPC) list(ctx context.Context, req discorddata.ModuleListRequest) discorddata.ModuleListReply {
	if !validGuildID(req.GuildID) {
		return discorddata.ModuleListReply{Error: repository.ErrInvalidInput.Error(), Code: discorddata.CodeInvalid}
	}
	rows, found, err := h.repo.ModulesGet(ctx, req.GuildID)
	if err != nil {
		message, code := failure(err)
		return discorddata.ModuleListReply{Error: message, Code: code}
	}
	return discorddata.ModuleListReply{Modules: rows, Found: found}
}

func (h moduleRPC) set(ctx context.Context, req discorddata.ModuleSetRequest) discorddata.ModuleSetReply {
	if bad := validateModuleSet(req); len(bad) > 0 {
		return discorddata.ModuleSetReply{Fields: bad, Error: repository.ErrInvalidInput.Error(), Code: discorddata.CodeInvalid}
	}
	version, err := h.repo.ModuleSet(ctx, repository.ModuleSetParams{
		GuildID:         req.GuildID,
		BroadcasterID:   req.BroadcasterID,
		Name:            req.Name,
		Enabled:         req.Enabled,
		Settings:        req.Settings,
		ExpectedVersion: req.ExpectedVersion,
	})
	if err != nil {
		message, code := failure(err)
		return discorddata.ModuleSetReply{Error: message, Code: code}
	}
	return discorddata.ModuleSetReply{Version: version}
}

func validGuildID(id string) bool { return id != "" && validSnowflakeValue(id) }

func validateModuleSet(req discorddata.ModuleSetRequest) []string {
	if !validGuildID(req.GuildID) {
		return []string{"guildId"}
	}
	var names []string
	for _, f := range ddiscord.ValidateModule(req.Name, req.Settings) {
		names = append(names, f.Field)
	}
	return names
}
