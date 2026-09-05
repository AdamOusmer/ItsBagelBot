// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bootstrap

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/discordapi"
)

type fakeRegistrar struct {
	app       discordapi.Snowflake
	appErr    error
	overwrite discordapi.CommandCatalog
	overErr   error
}

func (f *fakeRegistrar) GetCurrentApplication(context.Context) (discordapi.Snowflake, error) {
	return f.app, f.appErr
}
func (f *fakeRegistrar) BulkOverwriteCommands(_ context.Context, cat discordapi.CommandCatalog) error {
	f.overwrite = cat
	return f.overErr
}

func TestRegisterUsesTheLearnedApplicationID(t *testing.T) {
	r := &fakeRegistrar{app: discordapi.Snowflake{ID: "app-1"}}
	id, err := Register(context.Background(), r)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id != "app-1" {
		t.Fatalf("id = %s", id)
	}
	if r.overwrite.ApplicationID != "app-1" {
		t.Fatalf("catalog application id = %s", r.overwrite.ApplicationID)
	}
	if len(r.overwrite.Commands) == 0 {
		t.Fatal("expected a non-empty slash-command catalog")
	}
}

func TestRegisterFailsFastWhenApplicationLookupFails(t *testing.T) {
	r := &fakeRegistrar{appErr: errors.New("unauthorized")}
	if _, err := Register(context.Background(), r); err == nil {
		t.Fatal("expected an error")
	}
	if len(r.overwrite.Commands) != 0 {
		t.Fatal("must not attempt registration without an application id")
	}
}

func TestRegisterStillReturnsTheIDOnACatalogFailure(t *testing.T) {
	r := &fakeRegistrar{app: discordapi.Snowflake{ID: "app-1"}, overErr: errors.New("rate limited")}
	id, err := Register(context.Background(), r)
	if err == nil {
		t.Fatal("expected the catalog error to surface")
	}
	if id != "app-1" {
		t.Fatalf("id = %s, want app-1 even on a catalog failure", id)
	}
}

// The catalog is a bulk OVERWRITE: a subcommand missing from this list is
// deregistered at the next outgress boot and the engine handler behind it
// becomes unreachable with no error anywhere. Pinning the ticket group is what
// makes that a failing test rather than a silent regression.
func TestCatalogPinsTheTicketSubcommands(t *testing.T) {
	var ticket *discordapi.AppCommand
	for i, cmd := range Catalog() {
		if cmd.Name == "ticket" {
			ticket = &Catalog()[i]
			break
		}
	}
	if ticket == nil {
		t.Fatal("no /ticket command in the catalog")
	}
	want := map[string]bool{"open": false, "close": false, "claim": false, "add": false, "panel": false}
	for _, sub := range ticket.Options {
		if _, ok := want[sub.Name]; !ok {
			t.Fatalf("unexpected /ticket subcommand %q", sub.Name)
		}
		want[sub.Name] = true
		if sub.Type != 1 {
			t.Fatalf("/ticket %s type = %d, want 1 (SUB_COMMAND)", sub.Name, sub.Type)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("/ticket %s is missing from the catalog", name)
		}
	}
	add := subcommand(t, ticket, "add")
	if len(add.Options) != 1 || add.Options[0].Name != "user" || add.Options[0].Type != 6 || !add.Options[0].Required {
		t.Fatalf("/ticket add options = %+v, want one required USER option", add.Options)
	}
}

func subcommand(t *testing.T, cmd *discordapi.AppCommand, name string) discordapi.AppCommandOption {
	t.Helper()
	for _, sub := range cmd.Options {
		if sub.Name == name {
			return sub
		}
	}
	t.Fatalf("no /%s %s", cmd.Name, name)
	return discordapi.AppCommandOption{}
}
