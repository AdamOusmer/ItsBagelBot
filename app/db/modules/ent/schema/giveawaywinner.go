// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type GiveawayWinner struct {
	ent.Schema
}

func (GiveawayWinner) Fields() []ent.Field {
	millisecond := map[string]string{dialect.MySQL: "datetime(3)"}
	return []ent.Field{
		field.Uint64("user_id").Immutable(),
		field.String("round_id").NotEmpty().MaxLen(modulesrpc.GiveawayRoundIDMaxLen).Immutable(),
		field.Uint8("slot").Immutable(),
		field.String("prize").NotEmpty().MaxRuneLen(modulesrpc.GiveawayPrizeMaxLen).Immutable(),
		field.Time("won_at").SchemaType(millisecond).Immutable(),
		field.Uint64("viewer_id").Immutable(),
		field.String("viewer_login").MaxLen(modulesrpc.GiveawayLoginMaxLen).Immutable(),
		field.Bool("claimed").Default(false).Immutable(),
		field.Time("claimed_at").SchemaType(millisecond).Optional().Nillable().Immutable(),
		field.Time("delivered_at").SchemaType(millisecond).Optional().Nillable(),
	}
}

func (GiveawayWinner) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "round_id", "slot").Unique(),
		index.Fields("user_id", "won_at"),
		index.Fields("user_id", "viewer_id", "won_at"),
	}
}
