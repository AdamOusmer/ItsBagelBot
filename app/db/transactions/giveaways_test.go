package main

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/ent/enttest"
	"ItsBagelBot/app/db/transactions/ent/giveawayalert"
	"ItsBagelBot/app/db/transactions/web"
	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestBillingIncidentIsIdempotentAndFlagsLateCompletedCharge(t *testing.T) {
	db := enttest.Open(t, testdb.Driver, testdb.MemDSN(testdb.Name(t.Name())))
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	_, err := db.GiveawayAward.Create().SetID("award-late").SetGiveawayID("campaign").SetDrawID("draw").SetUserID(42).SetOrdinal(1).SetPrizeMonths(1).SetIntervalRule("verified").SetState("completed").SetBillingState("protected").SetConfirmedStart(start).SetConfirmedEnd(end).Save(ctx)
	require.NoError(t, err)
	incident := web.BillingIncident{EventID: "evt-late", EventType: "payment.completed", Action: "activate", TransactionID: "tx-late", UserID: 42, OccurredAt: start.Add(12 * time.Hour)}
	require.NoError(t, recordGiveawayBillingIncident(ctx, db, incident))
	require.NoError(t, recordGiveawayBillingIncident(ctx, db, incident))
	alerts, err := db.GiveawayAlert.Query().Where(giveawayalert.AwardIDEQ("award-late")).All(ctx)
	require.NoError(t, err)
	require.Len(t, alerts, 1)
	require.Equal(t, "evt-late", alerts[0].OperationID)
	_, err = db.GiveawayAlert.UpdateOneID(alerts[0].ID).SetState("resolved").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, recordGiveawayBillingIncident(ctx, db, incident))
	resolved := db.GiveawayAlert.GetX(ctx, alerts[0].ID)
	require.Equal(t, "resolved", resolved.State)
}
