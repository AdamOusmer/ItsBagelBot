// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import "testing"

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
	if !isInvite {
		t.Fatalf("baseline not recognized as invite")
	}
	for _, in := range inputs {
		got, invite := NormalizeLink(in)
		if !invite {
			t.Errorf("NormalizeLink(%q) isInvite = false, want true", in)
		}
		if got != want {
			t.Errorf("NormalizeLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeLinkDistinctInvites(t *testing.T) {
	a, _ := NormalizeLink("discord.gg/abc123")
	b, _ := NormalizeLink("discord.gg/xyz789")
	if a == b {
		t.Fatalf("distinct invite codes normalized to the same key: %q", a)
	}
}

func TestNormalizeLinkNonInviteURL(t *testing.T) {
	got, invite := NormalizeLink("https://example.com/scam?ref=123")
	if invite {
		t.Fatalf("example.com misclassified as an invite")
	}
	if got == "" {
		t.Fatalf("non-invite URL produced empty normalized key")
	}
	got2, _ := NormalizeLink("EXAMPLE.com/scam")
	if got != got2 {
		t.Errorf("non-invite URL host/path folding not case/scheme insensitive: %q != %q", got, got2)
	}
}

func TestNormalizeLinkInviteAndURLNeverCollide(t *testing.T) {
	invite, isInvite := NormalizeLink("discord.gg/x")
	generic, _ := NormalizeLink("example.com/x")
	if !isInvite {
		t.Fatalf("discord.gg/x not recognized as invite")
	}
	if invite == generic {
		t.Fatalf("invite and generic URL normalized to colliding keys")
	}
}

func TestNormalizeLinkEmpty(t *testing.T) {
	got, invite := NormalizeLink("   ")
	if got != "" || invite {
		t.Fatalf("blank input = (%q, %v), want (\"\", false)", got, invite)
	}
}

func TestNormalizeLinkNonInviteHostPathNotTreatedAsInvite(t *testing.T) {
	_, invite := NormalizeLink("https://discord.com/download")
	if invite {
		t.Fatalf("non-invite discord.com path misclassified as an invite")
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
