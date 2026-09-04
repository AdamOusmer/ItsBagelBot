// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"net/url"
	"strings"
)

// invitePrefix and urlPrefix tag a normalized link with which rule folded
// it, so an invite and a generic URL can never collide in the same Valkey
// key even if their paths happen to match, and so a Verdict's
// NormalizedLink is self-describing without a second IsInvite lookup.
const (
	invitePrefix = "invite:"
	urlPrefix    = "url:"
)

// trailingCutset is sentence punctuation a human leaves stuck to a pasted
// link ("check discord.gg/abc123." or "...(discord.gg/abc123)!") that is not
// part of the link itself. Stripped from the right end only, and repeatedly,
// so a run of it ("...!)") is fully removed rather than leaving one behind.
const trailingCutset = ".,!?)]}>:;\"'"

// inviteHosts maps a lowercased, www-stripped hostname to how its invite
// code is found in the path. discord.gg puts the code as the whole path;
// discord.com and discordapp.com (Discord's older domain, still live and
// still issued in some older embeds/bots) require the "/invite/" segment.
var inviteHosts = map[string]bool{
	"discord.gg":     true,
	"discord.com":    true,
	"discordapp.com": true,
}

// NormalizeLink folds every spelling of the same link onto one canonical
// string, and reports whether it recognized a Discord invite.
//
// Folded, regardless of input form: scheme presence, "www.", host case,
// path case, Discord's "<...>" embed-suppression brackets, a trailing query
// string or fragment, and trailing sentence punctuation. All of that must
// happen before the result is ever used as a Valkey key -- see the package
// doc's "why normalization happens before any counting" for what an
// attacker gets for free if it does not.
//
// Invite codes are lowercased along with the host. Discord invite codes are
// technically case-sensitive base62, so this can in principle fold two
// distinct real invites that differ only by letter case into one counter.
// That was a deliberate choice, not an oversight: a case-preserving
// normalizer hands an evader a trivial reset button (flip one letter's case
// each post, restart every threshold from zero), while two independently
// issued invites colliding under case-insensitive folding purely by chance
// is very unlikely and, worst case, costs a shared counter -- not a false
// action, since every threshold here already requires repetition beyond
// anything a single normal invite accrues.
//
// A link that is not a recognized Discord invite host still gets normalized
// (lowercased host, path with the query/fragment/trailing punctuation
// dropped) and tagged with the "url:" prefix instead of "invite:", so the
// same channel/author/fleet counting machinery applies to it without ever
// being mistaken for an invite count. See the package doc's "Non-invite
// links" section for how much less validated that path is.
func NormalizeLink(raw string) (normalized string, isInvite bool) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "<>")
	if trimmed == "" {
		return "", false
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		// Unparseable input still gets a stable (if crude) identity rather
		// than being dropped, so a malformed-but-repeated string is still
		// counted -- an attacker should not be able to evade detection
		// simply by feeding a string url.Parse chokes on.
		return urlPrefix + strings.ToLower(strings.TrimRight(trimmed, trailingCutset)), false
	}

	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if inviteHosts[host] {
		if code, ok := inviteCode(host, u.EscapedPath()); ok {
			return invitePrefix + strings.ToLower(strings.TrimRight(code, trailingCutset)), true
		}
	}

	path := strings.TrimRight(strings.ToLower(u.EscapedPath()), "/"+trailingCutset)
	return urlPrefix + host + path, false
}

// inviteCode extracts the invite code from a path already known to belong
// to an invite host. discord.gg carries the code as its entire path
// ("/abc123"); discord.com and discordapp.com require the "/invite/" path
// segment. A path with extra trailing segments (unexpected, but seen from
// some client-side link wrappers) still yields the first segment, since
// that is always where Discord itself puts the code.
func inviteCode(host, path string) (string, bool) {
	path = strings.Trim(path, "/")
	if path == "" {
		return "", false
	}
	if host == "discord.gg" {
		return firstSegment(path), true
	}
	const inviteSeg = "invite/"
	if !strings.HasPrefix(path, inviteSeg) {
		return "", false
	}
	rest := strings.TrimPrefix(path, inviteSeg)
	if rest == "" {
		return "", false
	}
	return firstSegment(rest), true
}

// InviteCode extracts a Discord invite code from raw exactly as written,
// case preserved -- unlike NormalizeLink's own return value, which folds
// case into the identity it counts on (deliberately, see NormalizeLink's
// doc). Discord invite codes are case-sensitive base62, so replaying a
// folded code against GET /invites/{code} risks a false 404 on a real,
// merely mixed-case invite. This exists solely for the caller
// (app/discord/engine/modules/linkguard.go's tripIsOwnInvite) that needs to
// hand a code to that endpoint; it shares NormalizeLink's own scheme/
// bracket stripping and inviteHosts/inviteCode host recognition so the two
// functions can never disagree about what counts as an invite, only about
// the casing of the result. ok is false for anything NormalizeLink would
// not report isInvite for.
func InviteCode(raw string) (code string, ok bool) {
	trimmed := strings.Trim(strings.TrimSpace(raw), "<>")
	if trimmed == "" {
		return "", false
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return "", false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if !inviteHosts[host] {
		return "", false
	}
	rawCode, ok := inviteCode(host, u.EscapedPath())
	if !ok {
		return "", false
	}
	return strings.TrimRight(rawCode, trailingCutset), true
}

func firstSegment(path string) string {
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}
