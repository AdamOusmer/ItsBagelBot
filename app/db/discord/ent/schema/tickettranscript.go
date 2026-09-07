// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// TicketTranscript is the rendered plain-text history of one closed ticket.
//
// It is a MySQL column, not an R2 object. The obvious objection is that
// transcripts are unbounded append-only text and the nightly backup is a full
// logical mysqldump, so a blob column turns every dump into a transcript
// export. Two things settle it the other way here: there is no R2/S3 client in
// this repo at all, so the object-store version means introducing a storage
// client plus its credential rotation for one feature; and the transcript must
// survive exactly as long as its ticket row, which a second store cannot
// promise without a reconciliation job nobody would write. Capped at 2 MiB by
// the RPC handler (roughly the 2000-message ceiling the engine paginates to),
// which bounds the dump growth to something the current ~1.1 MB dataset can
// absorb. Revisit if ticket volume ever makes the dump the binding constraint.
type TicketTranscript struct {
	ent.Schema
}

// Fields of the TicketTranscript.
func (TicketTranscript) Fields() []ent.Field {
	return []ent.Field{
		// LONGTEXT rather than TEXT: TEXT tops out at 64 KiB, which a busy
		// ticket passes in a few hundred messages.
		field.Text("body").
			SchemaType(map[string]string{"mysql": "LONGTEXT"}).
			Optional().
			Default(""),

		field.Int("message_count").Default(0),

		field.Time("stored_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Edges of the TicketTranscript.
func (TicketTranscript) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("ticket", Ticket.Type).
			Ref("transcript").
			Unique().
			Required(),
	}
}
