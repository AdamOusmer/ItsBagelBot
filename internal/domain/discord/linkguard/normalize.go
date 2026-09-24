// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package linkguard

import (
	"net/url"
	"strings"
)

const (
	invitePrefix = "invite:"
	urlPrefix    = "url:"
)

const trailingCutset = ".,!?)]}>:;\"'"

var inviteHosts = map[string]bool{
	"discord.gg":     true,
	"discord.com":    true,
	"discordapp.com": true,
}

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
