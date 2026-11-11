// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
	"time"
)

type AdminAuditDelivery struct{ ent.Schema }

func (AdminAuditDelivery) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", uuid.UUID{}).Default(uuid.New),
		field.JSON("payload", usersrpc.AuthRequest{}),
		field.Time("created_at").Default(time.Now),
	}
}
