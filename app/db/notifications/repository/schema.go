package repository

import (
	"ItsBagelBot/app/db/notifications/ent"
	atlas "ariga.io/atlas/sql/schema"
	"context"
	entschema "entgo.io/ent/dialect/sql/schema"
)

const obsoleteReadIndex = "notificationread_user_id_notification_reads"
const incarnationReadIndex = "notificationread_user_id_incarnation_notification_reads"

// MigrateSchema removes only the superseded read-receipt uniqueness constraint.
// Ent intentionally retains other unknown indexes during automatic migration.
func MigrateSchema(ctx context.Context, client *ent.Client) error {
	return client.Schema.Create(ctx, entschema.WithDiffHook(dropObsoleteReadIndex))
}
func dropObsoleteReadIndex(next entschema.Differ) entschema.Differ {
	return entschema.DiffFunc(func(current, desired *atlas.Schema) ([]atlas.Change, error) {
		// SQLite may rebuild a table when adding the incarnation column. Carry
		// unrelated indexes into that target so its rebuild also preserves them.
		if prior, ok := current.Table("notification_reads"); ok {
			if target, ok := desired.Table("notification_reads"); ok {
				for _, idx := range prior.Indexes {
					if idx.Name == obsoleteReadIndex {
						continue
					}
					if _, exists := target.Index(idx.Name); exists {
						continue
					}
					clone := *idx
					clone.Table = target
					clone.Parts = nil
					for _, part := range idx.Parts {
						copied := *part
						if part.C != nil {
							copied.C, _ = target.Column(part.C.Name)
						}
						clone.Parts = append(clone.Parts, &copied)
					}
					target.Indexes = append(target.Indexes, &clone)
				}
			}
		}
		changes, err := next.Diff(current, desired)
		if err != nil {
			return nil, err
		}
		table, ok := current.Table("notification_reads")
		if !ok {
			return changes, nil
		}
		old, ok := table.Index(obsoleteReadIndex)
		if !ok {
			return changes, nil
		}
		replacement, ok := desired.Table("notification_reads")
		if !ok {
			return changes, nil
		}
		if _, ok = replacement.Index(incarnationReadIndex); !ok {
			return changes, nil
		}
		for _, change := range changes {
			if modify, ok := change.(*atlas.ModifyTable); ok && modify.T.Name == table.Name {
				for _, c := range modify.Changes {
					if drop, ok := c.(*atlas.DropIndex); ok && drop.I.Name == obsoleteReadIndex {
						return changes, nil
					}
				}
				modify.Changes = append(modify.Changes, &atlas.DropIndex{I: old})
				return changes, nil
			}
		}
		return append(changes, &atlas.ModifyTable{T: replacement, Changes: []atlas.Change{&atlas.DropIndex{I: old}}}), nil
	})
}
