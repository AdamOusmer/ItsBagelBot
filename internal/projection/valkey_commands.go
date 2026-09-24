// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
)

const (
	commandFieldPrefix = "command:"
	aliasFieldPrefix   = "cmdalias:"
)

const commandsMarkerField = "commands:projected"

var commandsSection = sectionRead{prefix: commandFieldPrefix, marker: commandsMarkerField}

type CommandView = contract.CommandView

func commandViewFromEvent(dto data.CommandChangedDTO) CommandView {
	allowed := ""
	if dto.AllowedUserID != 0 {
		allowed = strconv.FormatUint(dto.AllowedUserID, 10)
	}
	return CommandView{
		Name:             dto.Name,
		Aliases:          dto.Aliases,
		Response:         dto.Response,
		IsActive:         dto.IsActive,
		StreamOnlineOnly: dto.StreamOnlineOnly,
		Perm:             dto.Perm,
		Cooldown:         dto.Cooldown,
		AllowedUserID:    allowed,
		Uses:             dto.Uses,
		BumpCounter:      dto.BumpCounter,
	}
}

func (v *Store) SetCommand(ctx context.Context, dto data.CommandChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	name := strings.ToLower(dto.Name)
	field := commandFieldPrefix + name

	cmds, err := v.retireStaleAliases(ctx, key, field)
	if err != nil {
		return err
	}

	if dto.Deleted {
		cmds = append(cmds, v.client.B().Hdel().Key(key).Field(field).Build())
		cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
		return v.pipeline(ctx, cmds...)
	}

	set, err := v.commandSetCommand(key, field, name, dto)
	if err != nil {
		return err
	}
	cmds = append(cmds, set)
	cmds = append(cmds, v.expiryCommands(key, DefaultTTL)...)
	return v.pipeline(ctx, cmds...)
}

func (v *Store) retireStaleAliases(ctx context.Context, key, field string) ([]valkey.Completed, error) {
	cmds := make([]valkey.Completed, 0, 4)
	old, err := v.primary.Do(ctx, v.primary.B().Hget().Key(key).Field(field).Build()).ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return cmds, nil
		}
		return nil, err
	}
	if old == "" {
		return cmds, nil
	}
	var prev CommandView
	if err := codec.Unmarshal([]byte(old), &prev); err != nil {
		return nil, err
	}
	if len(prev.Aliases) == 0 {
		return cmds, nil
	}
	stale := make([]string, 0, len(prev.Aliases))
	for _, a := range prev.Aliases {
		stale = append(stale, aliasFieldPrefix+strings.ToLower(a))
	}
	return append(cmds, v.client.B().Hdel().Key(key).Field(stale...).Build()), nil
}

func (v *Store) commandSetCommand(key, field, name string, dto data.CommandChangedDTO) (valkey.Completed, error) {
	view := commandViewFromEvent(dto)
	body, err := codec.Marshal(view)
	if err != nil {
		return valkey.Completed{}, err
	}
	set := v.client.B().Hset().Key(key).FieldValue().
		FieldValue(field, string(body))
	for _, a := range view.Aliases {
		set = set.FieldValue(aliasFieldPrefix+strings.ToLower(a), name)
	}
	return set.Build(), nil
}

func (v *Store) GetCommand(ctx context.Context, userID uint64, name string) (view CommandView, found bool, projected bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	lname := strings.ToLower(name)

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(commandFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field(aliasFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field(commandsMarkerField).Build(),
	)

	projected, err = markerProjected(res[2])
	if err != nil {
		return CommandView{}, false, false, err
	}

	view, found, err = decodeJSONField[CommandView](res[0])
	if err != nil {
		return CommandView{}, false, projected, err
	}
	if found {
		return view, true, true, nil
	}

	return v.resolveAlias(ctx, key, res[1], projected)
}

func (v *Store) resolveAlias(ctx context.Context, key string, aliasRes valkey.ValkeyResult, projected bool) (CommandView, bool, bool, error) {
	primary, err := aliasRes.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return CommandView{}, false, projected, nil
		}
		return CommandView{}, false, projected, err
	}
	if primary == "" {
		return CommandView{}, false, projected, nil
	}

	view, found, err := decodeJSONField[CommandView](v.client.Do(ctx, v.client.B().Hget().Key(key).Field(commandFieldPrefix+primary).Build()))
	if err != nil || !found {
		return CommandView{}, false, projected, err
	}
	return view, true, true, nil
}

func (v *Store) SetCommands(ctx context.Context, userID uint64, commands []CommandView) error {
	return v.SetCommandsWithTTL(ctx, userID, commands, DefaultTTL)
}

func (v *Store) SetCommandsWithTTL(ctx context.Context, userID uint64, commands []CommandView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	rows, err := commandRows(commands)
	if err != nil {
		return err
	}
	return v.replaceSection(ctx, userID, sectionWrite{
		prefixes: []string{commandFieldPrefix, aliasFieldPrefix},
		marker:   commandsMarkerField,
		ttl:      ttl,
		rows:     rows,
	})
}

func commandRows(commands []CommandView) ([][2]string, error) {
	rows := make([][2]string, 0, len(commands))
	for _, cmd := range commands {
		body, err := codec.Marshal(cmd)
		if err != nil {
			return nil, err
		}
		name := strings.ToLower(cmd.Name)
		rows = append(rows, [2]string{commandFieldPrefix + name, string(body)})
		for _, a := range cmd.Aliases {
			rows = append(rows, [2]string{aliasFieldPrefix + strings.ToLower(a), name})
		}
	}
	return rows, nil
}

func (v *Store) GetCommands(ctx context.Context, userID uint64) ([]CommandView, bool, error) {
	return getSection[CommandView](ctx, v, userID, commandsSection)
}
