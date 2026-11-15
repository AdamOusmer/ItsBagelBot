// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type FetchQuota struct {
	ent.Schema
}

func (FetchQuota) Fields() []ent.Field {
	return []ent.Field{field.Uint64("id").Immutable()}
}
