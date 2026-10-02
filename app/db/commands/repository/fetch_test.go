// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/ent/enttest"
	"ItsBagelBot/app/db/commands/ent/fetchdefinition"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"ItsBagelBot/internal/testdb"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

func fetchSetup(t *testing.T) (*ent.Client, *bustest.Publisher, *repository.Fetches) {
	t.Helper()

	client := testdb.Open(t, "fetchdefs", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })

	pub := bustest.NewPublisher()
	repo := repository.NewFetches(client, dbtest.NewPacker(t), pub, zap.NewNop())
	return client, pub, repo
}

func fetchSpec(name, url string) repository.FetchSpec {
	return repository.FetchSpec{Name: name, URL: url, IsActive: true}
}

func TestFetchKeySealUnsealRoundTrip(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	last4, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "openweather", Value: "sk-weather-secret-a1b2"})
	require.NoError(t, err)
	assert.Equal(t, "a1b2", last4)

	got, err := repo.Key(ctx, 1001, "openweather")
	require.NoError(t, err)
	assert.Equal(t, "sk-weather-secret-a1b2", got)

	row := client.FetchKey.Query().Where().OnlyX(ctx)
	assert.NotContains(t, string(row.KeyEnc), "sk-weather-secret", "key must be sealed at rest")
	assert.Equal(t, "a1b2", row.Last4)

	short, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "tiny", Value: "abc"})
	require.NoError(t, err)
	assert.Equal(t, "abc", short, "values shorter than four chars store as-is")
}

func TestFetchKeyAADBindsUserAndLabel(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	_, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "alpha", Value: "key-for-alpha"})
	require.NoError(t, err)
	row := client.FetchKey.Query().OnlyX(ctx)
	client.FetchKey.Create().SetUserID(1001).SetLabel("beta").SetLast4("0000").SetKeyEnc(row.KeyEnc).ExecX(ctx)

	_, err = repo.Key(ctx, 1001, "beta")
	assert.Error(t, err, "an envelope must not open under another label of the same user")
	assert.NotErrorIs(t, err, repository.ErrNoFetchKey)
	assert.NotErrorIs(t, err, repository.ErrCustodyUnavailable)

	client.FetchKey.Delete().ExecX(ctx)
	client.FetchKey.Create().SetUserID(2002).SetLabel("alpha").SetLast4("0000").SetKeyEnc(row.KeyEnc).ExecX(ctx)
	_, err = repo.Key(ctx, 2002, "alpha")
	assert.Error(t, err, "an envelope must not open under another user id")
}

func TestFetchKeyUpsertReplacesAndDeletes(t *testing.T) {
	_, _, repo := fetchSetup(t)
	ctx := context.Background()

	_, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "openweather", Value: "first"})
	require.NoError(t, err)
	_, err = repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "openweather", Value: "second-secret-99aa"})
	require.NoError(t, err)

	got, err := repo.Key(ctx, 1001, "openweather")
	require.NoError(t, err)
	assert.Equal(t, "second-secret-99aa", got, "rotation replaces the sealed value")

	keys, err := repo.ListKeys(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, keys, 1, "one row per label after rotation")
	assert.Equal(t, "99aa", keys[0].Last4)

	require.NoError(t, repo.DeleteKey(ctx, 1001, "openweather"))
	_, err = repo.Key(ctx, 1001, "openweather")
	assert.ErrorIs(t, err, repository.ErrNoFetchKey)

	require.NoError(t, repo.DeleteKey(ctx, 9999, "ghost"))
	_, err = repo.Key(ctx, 4242, "nope")
	assert.ErrorIs(t, err, repository.ErrNoFetchKey)
}

func TestFetchCustodyDisabledRefusesClosedButDefsWork(t *testing.T) {
	client := testdb.Open(t, "nocustody", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewFetches(client, nil, bustest.NewPublisher(), zap.NewNop())
	ctx := context.Background()

	assert.False(t, repo.CustodyEnabled())

	_, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "label", Value: "value"})
	assert.ErrorIs(t, err, repository.ErrCustodyUnavailable)
	_, err = repo.Key(ctx, 1001, "label")
	assert.ErrorIs(t, err, repository.ErrCustodyUnavailable)

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	views, err := repo.List(ctx, 1001)
	require.NoError(t, err)
	require.Len(t, views, 1)
}

func TestUpsertDefWritesImmediatelyAndPublishes(t *testing.T) {
	client, pub, repo := fetchSetup(t)
	ctx := context.Background()

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

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("defa", "https://api-a.example.com/v2")))

	count := client.FetchDefinition.Query().Where(fetchdefinition.UserIDEQ(1001)).CountX(ctx)
	assert.Equal(t, validate.MaxFetchDefsPerBroadcaster, count)

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

	err := repo.DeleteDef(ctx, 1001, repository.DefDelete{Name: "weather", Force: false})
	var refErr *repository.ErrFetchDefReferenced
	require.ErrorAs(t, err, &refErr)
	assert.Equal(t, []string{"temp"}, refErr.Commands)

	client.Commands.Delete().ExecX(ctx)
	client.Commands.Create().
		SetUserID(1001).
		SetName("other").
		SetResponse("{URLFETCH:WEATHER2} and {urlfetch:withered}").
		ExecX(ctx)
	require.NoError(t, repo.DeleteDef(ctx, 1001, repository.DefDelete{Name: "weather", Force: false}), "no real reference to weather")
	rows := client.FetchDefinition.Query().AllX(ctx)
	assert.Empty(t, rows)

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	client.Commands.Create().
		SetUserID(1001).
		SetName("temp").
		SetResponse("{urlfetch:wx}").
		ExecX(ctx)
	require.NoError(t, repo.DeleteDef(ctx, 1001, repository.DefDelete{Name: "wx", Force: true}))
	rows = client.FetchDefinition.Query().AllX(ctx)
	assert.Empty(t, rows)

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

func TestReferencingCommandsNamesOnlyWholeFetchTokens(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	var want []string
	for _, tc := range []struct {
		name       string
		response   string
		referenced bool
	}{
		{"fallback ends the payload", "it is {urlfetch:weather|n/a} out", true},
		{"the name folds on both sides", "{URLFETCH:Weather.a}", true},
		{"a dot-path still names the definition", "{urlfetch:weather.main.temp}", true},
		{"a bare reference matches", "{urlfetch:weather}", true},
		{"payloads are trimmed and unbanged", "{urlfetch: !Weather }", true},
		{"a longer name is a different definition", "{urlfetch:weather2}", false},
		{"a prefix is a different definition", "{urlfetch:weath}", false},
		{"a payload-free span names nothing", "{urlfetch}", false},
		{"an empty payload names nothing", "{urlfetch:}", false},
		{"a leading selector names nothing", "{urlfetch:.weather}", false},
		{"another token is not this one", "{counter:weather}", false},
		{"an unclosed brace is literal text", "{urlfetch:weather", false},
		{"the name is matched, not the text", "talking about urlfetch:weather", false},
	} {
		client.Commands.Create().SetUserID(1001).SetName(tc.name).SetResponse(tc.response).ExecX(ctx)
		if tc.referenced {
			want = append(want, tc.name)
		}
	}

	got, err := repo.ReferencingCommands(ctx, 1001, "weather")

	require.NoError(t, err)
	assert.ElementsMatch(t, want, got)
}

func TestRenameDefRetiresOldName(t *testing.T) {
	client, pub, repo := fetchSetup(t)
	ctx := context.Background()

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("old", "https://api.example.com")))
	require.NoError(t, repo.RenameDef(ctx, 1001, "old", fetchSpec("new", "https://api.example.com/v2")))

	row := client.FetchDefinition.Query().OnlyX(ctx)
	assert.Equal(t, "new", row.Name)
	assert.Equal(t, "https://api.example.com/v2", row.URL)

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
		{"old", false},
		{"old", true},
		{"new", false},
	}, got)

	require.NoError(t, repo.RenameDef(ctx, 1001, "ghost", fetchSpec("real", "https://real.example.com")))
	names2 := client.FetchDefinition.Query().AllX(ctx)
	require.Len(t, names2, 2)
}

func TestDeleteDefValidation(t *testing.T) {
	_, _, repo := fetchSetup(t)

	err := repo.DeleteDef(context.Background(), 1001, repository.DefDelete{Name: "bad name!"})
	assert.ErrorIs(t, err, validate.ErrFetchDefName)
}

func TestDeleteAllForUserClearsDefsAndKeys(t *testing.T) {
	client, _, repo := fetchSetup(t)
	ctx := context.Background()

	require.NoError(t, repo.UpsertDef(ctx, 1001, fetchSpec("wx", "https://api.example.com")))
	_, err := repo.SetKey(ctx, 1001, repository.KeyEntry{Label: "openweather", Value: "secret-value-zz09"})
	require.NoError(t, err)
	_, err = repo.SetKey(ctx, 2002, repository.KeyEntry{Label: "other", Value: "untouched-value"})
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
