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
	ticket := catalogCommand(t, "ticket")

	wantSubcommands(t, ticket, "open", "close", "claim", "add", "panel")
	wantRequiredUserOption(t, subcommand(t, ticket, "add"))
}

// catalogCommand is the top-level command named name, or a failed test: every
// assertion below is about a command the bulk overwrite still ships.
func catalogCommand(t *testing.T, name string) discordapi.AppCommand {
	t.Helper()
	for _, cmd := range Catalog() {
		if cmd.Name == name {
			return cmd
		}
	}
	t.Fatalf("no /%s command in the catalog", name)
	return discordapi.AppCommand{}
}

// wantSubcommands asserts cmd carries exactly names as its options, each a
// SUB_COMMAND. Both directions matter: a missing one is deregistered at the
// next boot, an extra one is a handler nobody wrote.
func wantSubcommands(t *testing.T, cmd discordapi.AppCommand, names ...string) {
	t.Helper()
	want := make(map[string]bool, len(names))
	for _, name := range names {
		want[name] = false
	}
	for _, sub := range cmd.Options {
		markSubcommand(t, cmd.Name, sub, want)
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("/%s %s is missing from the catalog", cmd.Name, name)
		}
	}
}

// markSubcommand ticks sub off the wanted set, refusing anything unexpected or
// registered as something other than a SUB_COMMAND (type 1).
func markSubcommand(t *testing.T, group string, sub discordapi.AppCommandOption, want map[string]bool) {
	t.Helper()
	if _, ok := want[sub.Name]; !ok {
		t.Fatalf("unexpected /%s subcommand %q", group, sub.Name)
	}
	want[sub.Name] = true
	if sub.Type != 1 {
		t.Fatalf("/%s %s type = %d, want 1 (SUB_COMMAND)", group, sub.Name, sub.Type)
	}
}

// wantRequiredUserOption pins /ticket add's one argument: a required USER
// (type 6). Without it Discord accepts the command with nobody to add.
func wantRequiredUserOption(t *testing.T, sub discordapi.AppCommandOption) {
	t.Helper()
	if len(sub.Options) != 1 {
		t.Fatalf("%s options = %+v, want exactly one", sub.Name, sub.Options)
	}
	opt := sub.Options[0]
	if opt.Name != "user" {
		t.Fatalf("%s option = %q, want user", sub.Name, opt.Name)
	}
	if opt.Type != 6 {
		t.Fatalf("%s user option type = %d, want 6 (USER)", sub.Name, opt.Type)
	}
	if !opt.Required {
		t.Fatalf("%s user option must be required", sub.Name)
	}
}

func subcommand(t *testing.T, cmd discordapi.AppCommand, name string) discordapi.AppCommandOption {
	t.Helper()
	for _, sub := range cmd.Options {
		if sub.Name == name {
			return sub
		}
	}
	t.Fatalf("no /%s %s", cmd.Name, name)
	return discordapi.AppCommandOption{}
}
