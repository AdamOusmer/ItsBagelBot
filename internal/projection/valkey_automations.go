// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
)

const automationFieldPrefix = "automation:"

const automationsMarkerField = "automations:projected"

var automationsSection = sectionRead{prefix: automationFieldPrefix, marker: automationsMarkerField}

func (v *Store) SetAutomations(ctx context.Context, userID uint64, automations []automation.Automation) error {
	defer segment(ctx, "HSET")()
	rows := make([][2]string, 0, len(automations))
	for _, a := range automations {
		body, err := codec.Marshal(a)
		if err != nil {
			return err
		}
		rows = append(rows, [2]string{automationFieldPrefix + a.ID, string(body)})
	}
	return v.replaceSection(ctx, userID, sectionWrite{
		prefixes: []string{automationFieldPrefix},
		marker:   automationsMarkerField,
		ttl:      DefaultTTL,
		rows:     rows,
	})
}

func (v *Store) ApplyAutomation(ctx context.Context, dto data.AutomationChangedDTO) error {
	defer segment(ctx, "HSET")()
	key := cache.UserKey(settingsKeyPrefix, dto.UserID)
	field := automationFieldPrefix + dto.ID
	if dto.Deleted || dto.Automation == nil {
		return v.pipeline(ctx, append([]valkey.Completed{v.client.B().Hdel().Key(key).Field(field).Build()}, v.expiryCommands(key, DefaultTTL)...)...)
	}
	body, err := codec.Marshal(dto.Automation)
	if err != nil {
		return err
	}
	set := v.client.B().Hset().Key(key).FieldValue().FieldValue(field, string(body)).Build()
	return v.pipeline(ctx, append([]valkey.Completed{set}, v.expiryCommands(key, DefaultTTL)...)...)
}

func (v *Store) GetAutomations(ctx context.Context, userID uint64) ([]automation.Automation, bool, error) {
	return getSection[automation.Automation](ctx, v, userID, automationsSection)
}

func (v *Store) AutomationsProjected(ctx context.Context, userID uint64) (bool, error) {
	defer segment(ctx, "HGET")()
	key := cache.UserKey(settingsKeyPrefix, userID)
	return markerProjected(v.client.Do(ctx, v.client.B().Hget().Key(key).Field(automationsMarkerField).Build()))
}
