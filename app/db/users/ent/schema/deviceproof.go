// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// DeviceProof is a short-lived, globally unique DPoP replay receipt.
type DeviceProof struct{ ent.Schema }

func (DeviceProof) Fields() []ent.Field {
	return []ent.Field{
		field.Bytes("proof_hash").Unique().Immutable().Sensitive().SchemaType(map[string]string{dialect.MySQL: "binary(32)"}),
		field.Time("expires_at"),
	}
}

func (DeviceProof) Indexes() []ent.Index {
	return []ent.Index{index.Fields("expires_at", "id")}
}
