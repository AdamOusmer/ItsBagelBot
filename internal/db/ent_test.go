package db_test

import (
	"ItsBagelBot/internal/db/ent"
	"ItsBagelBot/internal/db/ent/enttest"
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3" // Required for the in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setup creates a fresh, in-memory SQLite database for each test.
func setup(t *testing.T) *ent.Client {
	// "file:ent?mode=memory&cache=shared&_fk=1" tells SQLite to run in RAM
	// and enforce Foreign Keys (_fk=1).
	return enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
}

// Test 1: The "Cascade" (The most important test)
// Verifies that deleting a User automatically cleans up their Timers, Tokens, and Configs.
func TestCascadeDelete(t *testing.T) {
	client := setup(t)
	defer client.Close()
	ctx := context.Background()

	// 1. Create the Root User
	u, err := client.User.Create().
		SetID(12345).
		SetUsername("Mavey").
		SetEmail("mavey@concordia.ca").
		Save(ctx)
	require.NoError(t, err)

	// 2. Add Child Items
	_, err = client.Tokens.Create().
		SetUser(u).
		SetType("access_token").
		SetPlatform("twitch").
		SetToken("fake_token_123").
		Save(ctx)
	require.NoError(t, err)

	mockConfigJSON := []byte(`{"bot_enabled": "true"}`)

	_, err = client.Configs.Create().
		SetUser(u).
		SetConfigs(mockConfigJSON). // Correct field name from your schema
		Save(ctx)
	require.NoError(t, err)

	// --- FIX IS HERE ---
	_, err = client.Timers.Create().
		SetUser(u).
		SetName("water_break").
		SetCron("*/30 * * * *"). // Keeps the cron
		SetMessageThreshold(10).
		SetMessage("Time to hydrate!"). // ADDED: Required by schema
		// SetTimeThreshold(...)   // REMOVED: Field no longer exists
		Save(ctx)
	require.NoError(t, err)

	// 3. Verify they exist
	assert.Equal(t, 1, client.Tokens.Query().CountX(ctx))
	assert.Equal(t, 1, client.Configs.Query().CountX(ctx))
	assert.Equal(t, 1, client.Timers.Query().CountX(ctx))

	// 4. THE ACT: Delete the User
	err = client.User.DeleteOne(u).Exec(ctx)
	require.NoError(t, err)

	// 5. The Assertion
	assert.Equal(t, 0, client.Tokens.Query().CountX(ctx), "Tokens should be cascade deleted")
	assert.Equal(t, 0, client.Configs.Query().CountX(ctx), "Configs should be cascade deleted")
	assert.Equal(t, 0, client.Timers.Query().CountX(ctx), "Timers should be cascade deleted")
}

// Test 2: Token Uniqueness
// Verifies you cannot have two tokens of the same type/platform for one user.
func TestUniqueTokenConstraint(t *testing.T) {
	client := setup(t)
	defer client.Close()
	ctx := context.Background()

	// Create User
	u := client.User.Create().SetID(55).SetUsername("DupeTester").SetEmail("dupe@test.com").SaveX(ctx)

	// 1. Save First Token
	_, err := client.Tokens.Create().
		SetUser(u).
		SetType("access_token").
		SetPlatform("twitch").
		SetToken("token_one").
		Save(ctx)
	require.NoError(t, err)

	// 2. Try to Save Duplicate (Same User, Type, Platform)
	// This MUST fail because of your index: edges.To("user").Unique() combined with fields
	_, err = client.Tokens.Create().
		SetUser(u).
		SetType("access_token").
		SetPlatform("twitch").
		SetToken("token_two").
		Save(ctx)

	// 3. Assert Failure
	assert.Error(t, err, "Database should block duplicate tokens for same user/type/platform")
	assert.True(t, ent.IsConstraintError(err), "Error should be a constraint violation")
}

// Test 3: User Uniqueness
// Verifies you cannot have two users with the same email.
func TestUniqueUserEmail(t *testing.T) {
	client := setup(t)
	defer client.Close()
	ctx := context.Background()

	// 1. Create First User
	_, err := client.User.Create().
		SetID(1).
		SetUsername("UserA").
		SetEmail("unique@email.com").
		Save(ctx)
	require.NoError(t, err)

	// 2. Create Second User with SAME email
	_, err = client.User.Create().
		SetID(2).
		SetUsername("UserB").
		SetEmail("unique@email.com"). // Duplicate!
		Save(ctx)

	assert.Error(t, err)
	assert.True(t, ent.IsConstraintError(err))
}
