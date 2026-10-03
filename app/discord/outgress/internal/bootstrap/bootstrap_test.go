// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bootstrap_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/bootstrap"
	"ItsBagelBot/internal/discordapi"

	"github.com/stretchr/testify/require"
)

type fakeRegistrar struct {
	app        discordapi.Snowflake
	appErr     error
	catalogErr error
	registered discordapi.CommandCatalog
}

func (f *fakeRegistrar) GetCurrentApplication(context.Context) (discordapi.Snowflake, error) {
	return f.app, f.appErr
}

func (f *fakeRegistrar) BulkOverwriteCommands(_ context.Context, cat discordapi.CommandCatalog) error {
	f.registered = cat
	return f.catalogErr
}

func TestRegister(t *testing.T) {
	errUnauthorized, errRateLimited := errors.New("unauthorized"), errors.New("rate limited")
	cases := []struct {
		name           string
		registrar      fakeRegistrar
		wantID         string
		wantErr        error
		wantRegistered string
	}{{
		name:           "registers the catalog under the learned application id",
		registrar:      fakeRegistrar{app: discordapi.Snowflake{ID: "app-1"}},
		wantID:         "app-1",
		wantRegistered: "app-1",
	}, {
		name:      "registers nothing when the application lookup fails",
		registrar: fakeRegistrar{appErr: errUnauthorized},
		wantErr:   errUnauthorized,
	}, {
		name:           "still returns the application id when the catalog upload fails",
		registrar:      fakeRegistrar{app: discordapi.Snowflake{ID: "app-1"}, catalogErr: errRateLimited},
		wantID:         "app-1",
		wantErr:        errRateLimited,
		wantRegistered: "app-1",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.registrar

			id, err := bootstrap.Register(context.Background(), &r)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantID, id)
			require.Equal(t, tc.wantRegistered, r.registered.ApplicationID)
			require.Equal(t, tc.wantRegistered != "", len(r.registered.Commands) > 0)
		})
	}
}

func TestCatalogShipsTheTicketSubcommands(t *testing.T) {
	var ticket discordapi.AppCommand
	for _, cmd := range bootstrap.Catalog() {
		if cmd.Name == "ticket" {
			ticket = cmd
		}
	}

	subs := map[string][]discordapi.AppCommandOption{}
	for _, sub := range ticket.Options {
		require.Equal(t, 1, sub.Type, "/ticket %s must be a SUB_COMMAND", sub.Name)
		subs[sub.Name] = sub.Options
	}

	require.Equal(t, map[string][]discordapi.AppCommandOption{
		"open": nil, "close": nil, "claim": nil, "panel": nil,
		"add": {{Type: 6, Name: "user", Description: "Member", Required: true}},
	}, subs)
}
