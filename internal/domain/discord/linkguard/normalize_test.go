// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeLinkInviteEquivalence(t *testing.T) {
	inputs := []string{
		"discord.gg/abc123",
		"https://discord.gg/abc123",
		"http://discord.gg/abc123",
		"https://www.discord.gg/abc123",
		"DISCORD.GG/ABC123",
		"discord.com/invite/abc123",
		"https://discord.com/invite/abc123",
		"discordapp.com/invite/abc123",
		"https://discordapp.com/invite/abc123",
		"<https://discord.gg/abc123>",
		"https://discord.gg/abc123?event=999",
		"https://discord.gg/abc123.",
		"https://discord.gg/abc123)",
		"https://discord.gg/abc123!",
	}
	want, isInvite := NormalizeLink("discord.gg/abc123")
	require.True(t, isInvite, "baseline not recognized as invite")
	for _, in := range inputs {
		got, invite := NormalizeLink(in)
		assert.True(t, invite, in)
		assert.Equal(t, want, got, in)
	}
}

func TestNormalizeLinkKeying(t *testing.T) {
	cases := []struct {
		name     string
		a, b     string
		wantSame bool
	}{
		{"distinct invite codes never share a key", "discord.gg/abc123", "discord.gg/xyz789", false},
		{"an invite and a generic URL never collide", "discord.gg/x", "example.com/x", false},
		{"generic URLs ignore scheme, case and query", "https://example.com/scam?ref=123", "EXAMPLE.com/scam", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := NormalizeLink(tc.a)
			b, _ := NormalizeLink(tc.b)
			assert.NotEmpty(t, a)
			assert.Equal(t, tc.wantSame, a == b)
		})
	}
}

func TestNormalizeLinkClassification(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantInvite bool
		wantEmpty  bool
	}{
		{"an invite link is an invite", "discord.gg/x", true, false},
		{"a generic URL is not an invite", "https://example.com/scam?ref=123", false, false},
		{"a non-invite discord.com path is not an invite", "https://discord.com/download", false, false},
		{"blank input has no key", "   ", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, invite := NormalizeLink(tc.in)
			assert.Equal(t, tc.wantInvite, invite)
			assert.Equal(t, tc.wantEmpty, got == "")
		})
	}
}

func TestInviteCodePreservesCase(t *testing.T) {
	code, ok := InviteCode("https://discord.gg/AbC123XyZ")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if code != "AbC123XyZ" {
		t.Fatalf("code = %q, want case preserved AbC123XyZ", code)
	}
}

func TestInviteCodeDiscordComInviteSegment(t *testing.T) {
	code, ok := InviteCode("https://discord.com/invite/MixedCase1")
	if !ok || code != "MixedCase1" {
		t.Fatalf("InviteCode = (%q, %v), want (MixedCase1, true)", code, ok)
	}
}

func TestInviteCodeNonInviteLinkNotOK(t *testing.T) {
	if _, ok := InviteCode("https://example.com/AbC123"); ok {
		t.Fatal("ok = true for a non-invite host, want false")
	}
	if _, ok := InviteCode("https://discord.com/download"); ok {
		t.Fatal("ok = true for a non-invite discord.com path, want false")
	}
}
