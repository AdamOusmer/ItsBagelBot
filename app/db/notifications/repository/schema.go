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
		preserveReadIndexes(current, desired)
		changes, err := next.Diff(current, desired)
		if err != nil {
			return nil, err
		}
		drop := obsoleteReadIndexDrop(current, desired)
		if drop == nil {
			return changes, nil
		}
		return appendReadIndexDrop(changes, drop), nil
	})
}
func preserveReadIndexes(current, desired *atlas.Schema) {
	prior, found := current.Table("notification_reads")
	if !found {
		return
	}
	target, found := desired.Table("notification_reads")
	if !found {
		return
	}
	// SQLite can rebuild the table when adding the incarnation column. Carry
	// unrelated indexes into that target so the rebuild also preserves them.
	for _, index := range prior.Indexes {
		if index.Name == obsoleteReadIndex {
			continue
		}
		if _, exists := target.Index(index.Name); exists {
			continue
		}
		target.Indexes = append(target.Indexes, cloneReadIndex(index, target))
	}
}
func cloneReadIndex(index *atlas.Index, target *atlas.Table) *atlas.Index {
	clone := *index
	clone.Table = target
	clone.Parts = nil
	for _, part := range index.Parts {
		copied := *part
		if part.C != nil {
			copied.C, _ = target.Column(part.C.Name)
		}
		clone.Parts = append(clone.Parts, &copied)
	}
	return &clone
}
func obsoleteReadIndexDrop(current, desired *atlas.Schema) *atlas.ModifyTable {
	table, found := current.Table("notification_reads")
	if !found {
		return nil
	}
	old, found := table.Index(obsoleteReadIndex)
	if !found {
		return nil
	}
	replacement, found := desired.Table("notification_reads")
	if !found {
		return nil
	}
	if _, found = replacement.Index(incarnationReadIndex); !found {
		return nil
	}
	return &atlas.ModifyTable{T: replacement, Changes: []atlas.Change{&atlas.DropIndex{I: old}}}
}
func appendReadIndexDrop(changes []atlas.Change, drop *atlas.ModifyTable) []atlas.Change {
	for _, change := range changes {
		modify, ok := change.(*atlas.ModifyTable)
		if !ok {
			continue
		}
		if modify.T.Name != drop.T.Name {
			continue
		}
		if !hasObsoleteReadIndexDrop(modify) {
			modify.Changes = append(modify.Changes, drop.Changes...)
		}
		return changes
	}
	return append(changes, drop)
}
func hasObsoleteReadIndexDrop(modify *atlas.ModifyTable) bool {
	for _, change := range modify.Changes {
		drop, ok := change.(*atlas.DropIndex)
		if !ok {
			continue
		}
		if drop.I.Name == obsoleteReadIndex {
			return true
		}
	}
	return false
}
