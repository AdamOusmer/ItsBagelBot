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
