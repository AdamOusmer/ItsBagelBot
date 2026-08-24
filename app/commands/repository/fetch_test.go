// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"bytes"
	"context"
	"testing"

	"ItsBagelBot/app/commands/ent"
	"ItsBagelBot/app/commands/ent/enttest"
	"ItsBagelBot/app/commands/ent/fetchdefinition"
	"ItsBagelBot/app/commands/repository"
	fetchkeyrpc "ItsBagelBot/internal/domain/rpc/fetchkey"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/crypto"

	_ "github.com/mattn/go-sqlite3" // in-memory DB
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"

	"go.uber.org/zap"
)

// newFetchPacker fakes Tink exactly like the modules custody tests: a fresh
// in-memory AES256GCM keyset, serialized as the JSON NewCrypto consumes.
func newFetchPacker(t *testing.T) *crypto.Crypto {
	t.Helper()
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))
	packer, err := crypto.NewCrypto(buf.Bytes())
	require.NoError(t, err)
	return packer
}

func fetchSetup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Fetches) {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:fetchdefs?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	pub := bustest.NewPublisher()
	repo := repository.NewFetches(client, newFetchPacker(t), pub, zap.NewNop())
	return client, pub, repo
}

func fetchSpec(name, url string) repository.FetchSpec {
	return repository.FetchSpec{Name: name, URL: url, IsActive: true}
}

func TestFetchKeySealUnsealRoundTrip(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	last4, err := repo.SetKey(ctx, 1001, "openweather", "sk-weather-secret-a1b2")
	require.NoError(t, err)
	assert.Equal(t, "a1b2", last4)

	got, err := repo.Key(ctx, 1001, "openweather")
	require.NoError(t, err)
	assert.Equal(t, "sk-weather-secret-a1b2", got)

	// The plaintext must never sit in the column; the sealed blob and the
	// display suffix are all that persist.
	row := client.FetchKey.Query().Where().OnlyX(ctx)
	assert.NotContains(t, string(row.KeyEnc), "sk-weather-secret", "key must be sealed at rest")
	assert.Equal(t, "a1b2", row.Last4)
}

func TestFetchKeyLast4ShortValue(t *testing.T) {
	_, _, repo := fetchSetup(t)
	ctx := context.Background()

	last4, err := repo.SetKey(ctx, 1001, "tiny", "abc")
	require.NoError(t, err)
	assert.Equal(t, "abc", last4, "values shorter than four chars store as-is")
}

func TestFetchKeyAADBindsUserAndLabel(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	_, err := repo.SetKey(ctx, 1001, "alpha", "key-for-alpha")
	require.NoError(t, err)
	// Copy user 1001's alpha envelope onto the same user's OTHER label: the
	// AAD binds the label too, so it must fail to open rather than leak.
	row := client.FetchKey.Query().OnlyX(ctx)
	client.FetchKey.Create().SetUserID(1001).SetLabel("beta").SetLast4("0000").SetKeyEnc(row.KeyEnc).ExecX(ctx)

	_, err = repo.Key(ctx, 1001, "beta")
	assert.Error(t, err, "an envelope must not open under another label of the same user")
	assert.NotErrorIs(t, err, repository.ErrNoFetchKey)
	assert.NotErrorIs(t, err, repository.ErrCustodyUnavailable)

	// And onto another user's same-label row.
	client.FetchKey.Delete().ExecX(ctx)
	client.FetchKey.Create().SetUserID(2002).SetLabel("alpha").SetLast4("0000").SetKeyEnc(row.KeyEnc).ExecX(ctx)
	_, err = repo.Key(ctx, 2002, "alpha")
	assert.Error(t, err, "an envelope must not open under another user id")
}

func TestFetchKeyUpsertReplacesAndDeletes(t *testing.T) {
	_, _, repo := fetchSetup(t)
	ctx := context.Background()

	_, err := repo.SetKey(ctx, 1001, "openweather", "first")
	require.NoError(t, err)
	_, err = repo.SetKey(ctx, 1001, "openweather", "second-secret-99aa")
	require.NoError(t, err)

	got, err := repo.Key(ctx, 1001, "openweather")
	require.NoError(t, err)
	assert.Equal(t, "second-secret-99aa", got, "rotation replaces the sealed value")

	keys, err := repo.ListKeys(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, keys, 1, "one row per label after rotation")
	assert.Equal(t, "99aa", keys[0].Last4)

	// Key delete is always allowed, even with definitions still pointing at
	// the label (dangling labels fail closed at fetch time by design).
	require.NoError(t, repo.DeleteKey(ctx, 1001, "openweather"))
	_, err = repo.Key(ctx, 1001, "openweather")
	assert.ErrorIs(t, err, repository.ErrNoFetchKey)

	// Deleting an absent key is a no-op, like the govee clear path.
	require.NoError(t, repo.DeleteKey(ctx, 9999, "ghost"))
}

func TestFetchKeyMissingMapsToErrNoFetchKey(t *testing.T) {
	_, _, repo := fetchSetup(t)
	_, err := repo.Key(context.Background(), 4242, "nope")
	assert.ErrorIs(t, err, repository.ErrNoFetchKey)
}

func TestFetchCustodyDisabledRefusesClosedButDefsWork(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:nocustody?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	repo := repository.NewFetches(client, nil, bustest.NewPublisher(), zap.NewNop())
	ctx := context.Background()

	assert.False(t, repo.CustodyEnabled())

	_, err := repo.SetKey(ctx, 1001, "label", "value")
	assert.ErrorIs(t, err, repository.ErrCustodyUnavailable)
	_, err = repo.Key(ctx, 1001, "label")
	assert.ErrorIs(t, err, repository.ErrCustodyUnavailable)

	// Definitions keep working keyless — this is why the keyset load is
	// best-effort instead of fatal.
	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	views, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
}

func TestUpsertDefWritesImmediatelyAndPublishes(t *testing.T) {
	client, pub, repo := fetchSetup(t)
	ctx := context.Background()

	// Immediate write: no batcher window to wait out.
	require.NoError(t, repo.UpsertDef(ctx, 1001, repository.FetchSpec{
		Name:     "!Weather",
		URL:      "https://api.example.com/v1?city=berlin",
		Path:     []string{"current", "temp_c"},
		KeyLabel: "openweather",
		IsActive: true,
	}))

	row := client.FetchDefinition.Query().Where(fetchdefinition.UserID(1001)).OnlyX(ctx)
	assert.Equal(t, "weather", row.Name, "normalized bare lower-case name")
	assert.Equal(t, "openweather", row.KeyLabel)

	msgs := pub.On("data.commands.fetch_changed")
	require.Len(t, msgs, 1)
	var dto struct {
		UserID   uint64   `json:"user_id"`
		Name     string   `json:"name"`
		URL      string   `json:"url"`
		JSONPath []string `json:"json_path"`
		IsActive bool     `json:"is_active"`
	}
	require.NoError(t, codec.Unmarshal(msgs[0].Payload, &dto))
	assert.Equal(t, uint64(1001), dto.UserID)
	assert.Equal(t, "weather", dto.Name)
	assert.Equal(t, []string{"current", "temp_c"}, dto.JSONPath)

	// A second save of the same name updates in place without a second row.
	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("Weather", "https://api.example.com/v2")))
	rows := client.FetchDefinition.Query().AllX(ctx)
	require.Len(t, rows, 1)
	assert.Equal(t, "https://api.example.com/v2", rows[0].URL)
}

func TestUpsertDefValidation(t *testing.T) {
	_, _, repo := fetchSetup(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		spec    repository.FetchSpec
		wantErr error
	}{
		{"bad name charset", repository.FetchSpec{Name: "Top Games!", URL: "https://x.example.com"}, validate.ErrFetchDefName},
		{"http url", fetchSpec("wx", "http://api.example.com"), validate.ErrFetchURL},
		{"ip literal url", fetchSpec("wx", "https://127.0.0.1/admin"), validate.ErrFetchHost},
		{"localhost url", fetchSpec("wx", "https://localhost/api"), validate.ErrFetchHost},
		{"bad path segment", repository.FetchSpec{Name: "wx", URL: "https://x.example.com", Path: []string{"a.b"}}, validate.ErrFetchPath},
		{"path too deep", repository.FetchSpec{Name: "wx", URL: "https://x.example.com", Path: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}}, validate.ErrFetchPath},
		{"bad key label", repository.FetchSpec{Name: "wx", URL: "https://x.example.com", KeyLabel: "has space"}, validate.ErrKeyLabel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := repo.UpsertDef(ctx, 1001, tc.spec)
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestFetchDefQuotaEnforcedSynchronously(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	for i := 0; i < validate.MaxFetchDefsPerBroadcaster; i++ {
		spec := fetchSpec("def"+string(rune('a'+i)), "https://api"+string(rune('a'+i))+".example.com")
		require.NoError(t, repo.UpsertDef(ctx, 1001, spec), "def %d should fit under the cap", i)
	}

	err := repo.UpsertDef(ctx, 1001, fetchSpec("overflow", "https://overflow.example.com"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "limit reached")

	// The cap is creation-scoped: editing an existing definition is never
	// quota-blocked.
	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("defa", "https://api-a.example.com/v2")))

	count := client.FetchDefinition.Query().Where(fetchdefinition.UserIDEQ(1001)).CountX(ctx)
	assert.Equal(t, validate.MaxFetchDefsPerBroadcaster, count)

	// Another broadcaster's cap is independent.
	require.NoError(t, repo.UpsertDef(ctx, 2002, fetchSpec("defa", "https://api-a.example.com")))
}

func TestDeleteDefReferenceGate(t *testing.T) {
	client, pub, repo := fetchSetup(t)
	ctx := context.Background()

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("weather", "https://api.example.com")))
	client.Commands.Create().
		SetUserID(1001).
		SetName("temp").
		SetResponse("It is {urlfetch:weather.current.temp_c} right now").
		ExecX(ctx)
	client.Commands.Create().
		SetUserID(1001).
		SetName("unrelated").
		SetResponse("hello world").
		ExecX(ctx)

	// Refused while referenced; the refusal names the referencing commands.
	err := repo.DeleteDef(ctx, 1001, "weather", false)
	var refErr *repository.ErrFetchDefReferenced
	require.ErrorAs(t, err, &refErr)
	assert.Equal(t, []string{"temp"}, refErr.Commands)

	// Case-insensitive on the token name, precise on token boundaries:
	// "{urlfetch:weather2}" must NOT count as a reference to "weather".
	client.Commands.Delete().ExecX(ctx)
	client.Commands.Create().
		SetUserID(1001).
		SetName("other").
		SetResponse("{URLFETCH:WEATHER2} and {urlfetch:withered}").
		ExecX(ctx)
	require.NoError(t, repo.DeleteDef(ctx, 1001, "weather", false), "no real reference to weather")
	rows := client.FetchDefinition.Query().AllX(ctx)
	assert.Empty(t, rows)

	// Force bypasses the gate.
	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	client.Commands.Create().
		SetUserID(1001).
		SetName("temp").
		SetResponse("{urlfetch:wx}").
		ExecX(ctx)
	require.NoError(t, repo.DeleteDef(ctx, 1001, "wx", true))
	rows = client.FetchDefinition.Query().AllX(ctx)
	assert.Empty(t, rows)

	// Every successful delete announced Deleted:true so the projector HDELs.
	finalDel := map[string]bool{}
	for _, msg := range pub.On("data.commands.fetch_changed") {
		var dto struct {
			Name    string `json:"name"`
			Deleted bool   `json:"deleted"`
		}
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		if dto.Name == "wx" || dto.Name == "weather" {
			finalDel[dto.Name] = dto.Deleted
		}
	}
	assert.True(t, finalDel["weather"], "weather's last event must carry Deleted")
	assert.True(t, finalDel["wx"], "wx's last event must carry Deleted")
}

func TestRenameDefRetiresOldName(t *testing.T) {
	client, pub, repo := fetchSetup(t)
	ctx := context.Background()

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("old", "https://api.example.com")))
	require.NoError(t, repo.RenameDef(ctx, 1001, "old", fetchSpec("new", "https://api.example.com/v2")))

	row := client.FetchDefinition.Query().OnlyX(ctx)
	assert.Equal(t, "new", row.Name)
	assert.Equal(t, "https://api.example.com/v2", row.URL)

	// Rename publishes delete(old) + change(new) so consumers retire fetch:old.
	// The initial create's change event precedes them on the same subject.
	msgs := pub.On("data.commands.fetch_changed")
	require.Len(t, msgs, 3)
	type nameDel struct {
		name    string
		deleted bool
	}
	var got []nameDel
	for _, msg := range msgs {
		var dto struct {
			Name    string `json:"name"`
			Deleted bool   `json:"deleted"`
		}
		require.NoError(t, codec.Unmarshal(msg.Payload, &dto))
		got = append(got, nameDel{dto.Name, dto.Deleted})
	}
	assert.Equal(t, []nameDel{
		{"old", false}, // create
		{"old", true},  // rename retires the old field
		{"new", false}, // rename writes the new one
	}, got)

	// Renaming a ghost falls back to a plain upsert so the edit is not lost.
	require.NoError(t, repo.RenameDef(ctx, 1001, "ghost", fetchSpec("real", "https://real.example.com")))
	names2 := client.FetchDefinition.Query().AllX(ctx)
	require.Len(t, names2, 2)
}

func TestDeleteDefValidation(t *testing.T) {
	_, _, repo := fetchSetup(t)

	err := repo.DeleteDef(context.Background(), 1001, "bad name!", false)
	assert.ErrorIs(t, err, validate.ErrFetchDefName)
}

func TestDeleteAllForUserClearsDefsAndKeys(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	_, err := repo.SetKey(ctx, 1001, "openweather", "secret-value-zz09")
	require.NoError(t, err)
	_, err = repo.SetKey(ctx, 2002, "other", "untouched-value")
	require.NoError(t, err)

	require.NoError(t, repo.DeleteAllForUser(ctx, 1001))

	assert.Zero(t, client.FetchDefinition.Query().Where(fetchdefinition.UserIDEQ(1001)).CountX(ctx))
	assert.Zero(t, countKeysFor(client, 1001), "keys for 1001 gone")
	remaining := countKeysFor(client, 2002)
	assert.Equal(t, 1, remaining, "other users untouched")
}

func countKeysFor(client *ent.Client, userID uint64) int {
	rows := client.FetchKey.Query().AllX(context.Background())
	n := 0
	for _, r := range rows {
		if r.UserID == userID {
			n++
		}
	}
	return n
}

func TestFetchViewWireTagsMatchProjectionContract(t *testing.T) {
	// The Valkey field JSON and the RPC reply share one shape; pin the tags so
	// they cannot drift apart silently (CommandView/contract precedent).
	view := fetchkeyrpc.FetchView{Name: "wx", URL: "https://x", JSONPath: []string{"a"}, KeyLabel: "k", IsActive: true}
	b, err := codec.Marshal(view)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"wx","url":"https://x","json_path":["a"],"key_label":"k","is_active":true}`, string(b))

	minimal := fetchkeyrpc.FetchView{Name: "plain", URL: "https://y"}
	b, err = codec.Marshal(minimal)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"plain","url":"https://y","is_active":false}`, string(b),
		"omitted fields must stay absent from the projected JSON")
}
