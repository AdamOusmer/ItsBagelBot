package repository_test

import (
	"ItsBagelBot/app/db/notifications/ent"
	"ItsBagelBot/app/db/notifications/ent/notification"
	"ItsBagelBot/app/db/notifications/ent/notificationread"
	"ItsBagelBot/app/db/notifications/repository"
	"ItsBagelBot/internal/testdb"
	"context"
	"database/sql"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestReadIncarnationMigrationUpgradesOnlyObsoleteIndex(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open(testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	require.NoError(t, err)
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	t.Cleanup(func() { client.Close() })
	// Start with the pre-incarnation read table, including its exact old unique index.

	require.NoError(t, client.Schema.Create(ctx, entschema.WithHooks(legacyReadSchema)))
	parent := client.Notification.Create().SetScope(notification.ScopeBroadcast).SetTitle("announcement").SetBody("body").SetCreatedBy(1).SetCreatedByLogin("admin").SaveX(ctx)
	_, err = db.ExecContext(ctx, "INSERT INTO notification_reads (user_id,read_at,notification_reads) VALUES (?,?,?)", alice, time.Now().Add(-time.Hour), parent.ID)
	require.NoError(t, err)
	// An independently maintained index must survive this narrowly scoped upgrade.
	_, err = db.ExecContext(ctx, "CREATE INDEX keep_read_history_index ON notification_reads (read_at)")
	require.NoError(t, err)
	require.NoError(t, repository.MigrateSchema(ctx, client))
	require.NoError(t, repository.MigrateSchema(ctx, client), "upgrade is idempotent")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_index_list('notification_reads') WHERE name='notificationread_user_id_notification_reads'").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_index_list('notification_reads') WHERE name='keep_read_history_index'").Scan(&count))
	require.Equal(t, 1, count)
	repo := repository.New(client)
	require.NoError(t, repo.MarkReadForIncarnation(ctx, parent.ID, repository.Recipient{UserID: alice, Incarnation: "123"}, time.Now().Add(time.Hour)))
	require.NoError(t, repo.MarkReadForIncarnation(ctx, parent.ID, repository.Recipient{UserID: alice, Incarnation: "456"}, time.Now().Add(time.Hour)))
	require.Equal(t, 3, client.NotificationRead.Query().Where(notificationread.UserIDEQ(alice)).CountX(ctx), "legacy and replacement acknowledgments coexist without rebinding another incarnation")
}

func legacyReadSchema(next entschema.Creator) entschema.Creator {
	return entschema.CreateFunc(func(ctx context.Context, tables ...*entschema.Table) error {
		migrated := make([]*entschema.Table, len(tables))
		for i, table := range tables {
			migrated[i] = legacyReadTable(table)
		}
		return next.Create(ctx, migrated...)
	})
}
func legacyReadTable(table *entschema.Table) *entschema.Table {
	if table.Name != "notification_reads" {
		return table
	}
	clone := *table
	clone.Columns = nil
	for _, column := range table.Columns {
		if column.Name != "incarnation" {
			clone.Columns = append(clone.Columns, column)
		}
	}
	clone.Indexes = nil
	clone.AddIndex("notificationread_user_id_notification_reads", true, []string{"user_id", "notification_reads"})
	return &clone
}
