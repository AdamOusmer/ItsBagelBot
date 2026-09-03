// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

// Command projection: the command:<name> rows, the cmdalias:<alias> pointers
// that resolve an alias in one extra HGET, their commands:projected marker and
// the readers the chat hot path goes through. Split from valkey.go so the
// command section reads as one file beside the module, stream and fetch
// sections (the valkey_fetch.go precedent).

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

// Command rows are addressable individually so a single lookup is one HGET, not
// a whole-hash HGETALL. commandFieldPrefix holds the command JSON keyed by its
// lower-cased primary name; aliasFieldPrefix holds alias -> primary-name
// pointers so an alias resolves in one extra HGET without scanning every row.
const (
	commandFieldPrefix = "command:"
	aliasFieldPrefix   = "cmdalias:"
)

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
	}
}

// SetCommand projects one command row of one user. The command JSON lands under
// command:<name> and one alias:<alias> pointer is written per alias. The event
// carries only the new aliases, so the previous row is read first to retire any
// alias pointers that no longer apply (rename, reword, delete). That extra HGET
// is on the rare write path; the hot read path stays a single HGET.
func (v *Store) SetCommand(ctx context.Context, dto data.CommandChangedDTO) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	name := strings.ToLower(dto.Name)
	field := commandFieldPrefix + name

	// The event carries only the new aliases, so retire the previous row's
	// alias pointers (rename, reword, delete) first. That extra HGET is on the
	// rare write path; the hot read path stays a single HGET.
	cmds := v.retireStaleAliases(ctx, key, field)

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

// retireStaleAliases reads the command's previous row and returns the HDEL (if
// any) that removes the alias pointers it no longer carries.
//
// This read is pinned to the primary: it is the read half of a
// read-modify-write over the row SetCommand is about to overwrite. A node-local
// replica that has not yet received the previous SetCommand returns an empty or
// older row, so the HDEL is either skipped or computed from the wrong alias
// list, and the retired aliases stay resolvable forever. Unlike a stale cache
// read this never converges, because nothing revisits the row. Only the reading
// side is pinned; the rest of the Store keeps node-local reads.
func (v *Store) retireStaleAliases(ctx context.Context, key, field string) []valkey.Completed {
	cmds := make([]valkey.Completed, 0, 4)
	old, _ := v.primary.Do(ctx, v.primary.B().Hget().Key(key).Field(field).Build()).ToString()
	if old == "" {
		return cmds
	}
	var prev CommandView
	if codec.Unmarshal([]byte(old), &prev) != nil || len(prev.Aliases) == 0 {
		return cmds
	}
	stale := make([]string, 0, len(prev.Aliases))
	for _, a := range prev.Aliases {
		stale = append(stale, aliasFieldPrefix+strings.ToLower(a))
	}
	return append(cmds, v.client.B().Hdel().Key(key).Field(stale...).Build())
}

// commandSetCommand builds the HSET writing the command body plus its alias
// pointers. Like SetModule, it never sets commands:projected: a single event
// row on a cold hash must not make the command section read as complete —
// only SetCommands / SetCommandsWithTTL (full-list writes) set the marker.
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

// GetCommand reads one command by the name (or alias) a viewer typed, in a
// single round trip. found reports whether the command exists; projected
// reports whether the command section has been populated at all, so a caller
// can tell a real "no such command" from a cold Valkey miss that should fall
// through to the projector RPC.
func (v *Store) GetCommand(ctx context.Context, userID uint64, name string) (view CommandView, found bool, projected bool, err error) {
	defer segment(ctx, "HGET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	lname := strings.ToLower(name)

	res := v.client.DoMulti(ctx,
		v.client.B().Hget().Key(key).Field(commandFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field(aliasFieldPrefix+lname).Build(),
		v.client.B().Hget().Key(key).Field("commands:projected").Build(),
	)

	projected, err = markerProjected(res[2])
	if err != nil {
		return CommandView{}, false, false, err
	}

	// Direct hit: the typed name is a command's own field.
	view, found, err = decodeJSONField[CommandView](res[0])
	if err != nil {
		return CommandView{}, false, projected, err
	}
	if found {
		return view, true, true, nil
	}

	// Alias hit: the typed name points at another command's field, read next.
	return v.resolveAlias(ctx, key, res[1], projected)
}

// resolveAlias follows an alias pointer to the command it names and reads that
// command's body in one more round trip. A missing or dangling alias is a clean
// miss.
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

// SetCommands projects a complete command list and records that an empty list is
// known data, not a cold Valkey miss.
func (v *Store) SetCommands(ctx context.Context, userID uint64, commands []CommandView) error {
	return v.SetCommandsWithTTL(ctx, userID, commands, DefaultTTL)
}

// SetCommandsWithTTL replaces the complete command section and keeps the hash
// for at least ttl. An empty list is still marked as projected.
func (v *Store) SetCommandsWithTTL(ctx context.Context, userID uint64, commands []CommandView, ttl time.Duration) error {
	defer segment(ctx, "HSET")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	if err := v.clearProjectionFields(ctx, key, commandFieldPrefix, aliasFieldPrefix); err != nil {
		return err
	}

	fields := v.client.B().Hset().
		Key(key).
		FieldValue().
		FieldValue("commands:projected", "1")

	for _, cmd := range commands {
		body, err := codec.Marshal(cmd)
		if err != nil {
			return err
		}
		name := strings.ToLower(cmd.Name)
		fields = fields.FieldValue(commandFieldPrefix+name, string(body))
		for _, a := range cmd.Aliases {
			fields = fields.FieldValue(aliasFieldPrefix+strings.ToLower(a), name)
		}
	}

	return v.pipelineWithTTL(ctx, key, ttl, fields.Build())
}

func (v *Store) GetCommands(ctx context.Context, userID uint64) ([]CommandView, bool, error) {
	defer segment(ctx, "HGETALL")()

	key := cache.UserKey(settingsKeyPrefix, userID)
	fields, err := v.client.Do(ctx, v.client.B().Hgetall().Key(key).Build()).AsStrMap()
	if err != nil {
		return nil, false, err
	}

	// projected trusts the marker alone (see GetModules): per-command event
	// rows never set it, so a partial hash falls through to full hydration.
	projected := fields["commands:projected"] == "1"
	out := make([]CommandView, 0)
	for field, value := range fields {
		name, ok := strings.CutPrefix(field, "command:")
		if !ok || name == "" {
			continue
		}
		var cmd CommandView
		if err := codec.Unmarshal([]byte(value), &cmd); err != nil {
			continue
		}
		out = append(out, cmd)
	}
	return out, projected, nil
}
