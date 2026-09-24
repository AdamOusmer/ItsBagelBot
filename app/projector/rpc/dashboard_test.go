// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"slices"
	"testing"

	"ItsBagelBot/internal/projection"
)

func TestCommandKeys(t *testing.T) {
	commands := []projection.CommandView{
		{Name: "uptime", Aliases: []string{"live", "up"}},
		{Name: "socials"},
		{Name: "lurk", Aliases: []string{"hide"}},
	}

	got := commandKeys(commands)
	want := []string{"uptime", "live", "up", "socials", "lurk", "hide"}
	if !slices.Equal(got, want) {
		t.Fatalf("commandKeys() = %v, want %v", got, want)
	}
}

func TestCommandKeysEmpty(t *testing.T) {
	if got := commandKeys(nil); len(got) != 0 {
		t.Fatalf("commandKeys(nil) = %v, want empty", got)
	}
}
