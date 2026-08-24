// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// $(urlfetch) input rules, shared by the commands service (authoritative
// save-time check), the console (instant client feedback over the same Error
// strings) and gossip (fetch-time re-checks). Every number here is part of
// that three-way contract; changing one changes all three surfaces.

const (
	// MaxFetchDefsPerBroadcaster caps definitions per broadcaster at 20. This
	// is a placeholder default pending traffic modelling (docs/urlfetch
	// follow-ups) — but it is not arbitrary: sesame pre-resolves every
	// {urlfetch:name} token of one chat message concurrently, so 20 defs is
	// also the per-message worst-case fan-out upper bound we sized the
	// 3.5s/2.5s fetch budgets and the per-host buckets against. Unlimited was
	// rejected outright (a broadcaster-authored fetch is unbounded abuse
	// surface); 50 was rejected because nothing in the authoring UX needs it
	// and it would double the quota COUNT scan for no observed demand.
	MaxFetchDefsPerBroadcaster = 20

	// MaxFetchURLLength bounds the stored URL at 512 chars: long enough for
	// signed query strings (S3 SigV4, Mapbox, WeatherAPI all land in the
	// 200-400 range once signed), short enough that the URL stays a single
	// Valkey hash field and a log line. The alternative — 2048 "because
	// browsers" — buys nothing: tokens carrying multi-KB URLs would blow the
	// chat-line budget downstream anyway.
	MaxFetchURLLength = 512

	// maxFetchPathDepth caps json_path at 8 segments. Deepest path seen in a
	// sample of public weather/stats APIs is 5 ($.data.forecast.forecastday[0].day
	// class); 8 leaves headroom without making the picker tree unbounded.
	// Unlimited was rejected: depth drives resolver recursion on gossip's hot
	// path, and a hostile def could bury CPU in parse-and-walk for no authoring
	// benefit.
	maxFetchPathDepth = 8

	// MaxKeyLabelLength / MaxKeyValueLength bound custody rows: the label is
	// half of the unique key and of the AAD string; the value cap keeps one
	// sealed blob small (Tink AEAD encrypts in memory) and outlaws pasting
	// whole JSON config dumps where an API key belongs.
	MaxKeyLabelLength = 32
	MaxKeyValueLength = 512
)

var (
	// ErrFetchDefName rejects names outside ^[a-z0-9_]{1,32}$ — the name is
	// embedded into the Valkey hash field "fetch:<name>" and into the
	// {urlfetch:<name>} token grammar, both of which parse on ':' and '}'.
	ErrFetchDefName = errors.New("fetch name must be 1-32 characters of [a-z0-9_]")
	// ErrFetchURL covers scheme, length and parse failures; ErrFetchHost is
	// the host denylist, kept separate so the console can point the author at
	// the right form field error.
	ErrFetchURL  = errors.New("fetch url must be an absolute https url of at most 512 characters")
	ErrFetchHost = errors.New("fetch url host must be a public dns name (no ip literals, localhost, .local or .internal)")
	// ErrFetchPath rejects json_path segments outside [A-Za-z0-9_-] or paths
	// deeper than 8.
	ErrFetchPath = errors.New("json path segments must be [A-Za-z0-9_-], at most 8 deep")
	ErrKeyLabel  = errors.New("key label must be 1-32 printable ascii characters")
	ErrKeyValue  = errors.New("key value must be 1-512 characters")
)

// FetchDefName validates a definition name: lower-case letters, digits and
// underscores, 1-32 chars — the same grammar the console slugifies to. The
// immovable floor applies too: the name can end up echoed inside error lists
// of referencing commands and in audit trails.
//
// The floor runs over the name with underscores folded to spaces: CheckFloor's
// hate scan is word-bounded, so without the fold "chat_<slur>" glues into one
// token and releases. Underscores are the ONLY legal separator here, so this
// cannot glue tokens that were ever distinct words; it can only un-glue what
// the author glued.
func FetchDefName(name string) error {
	if len(name) == 0 || len(name) > 32 {
		return ErrFetchDefName
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_':
		default:
			// Upper-case included: callers normalize before validating, so an
			// upper-case letter here means a bypass, not an un-normalized save.
			return ErrFetchDefName
		}
	}
	return FloorClean(strings.ReplaceAll(name, "_", " "))
}

// FetchURL validates a definition's endpoint at save time: absolute https,
// ≤512 chars, public-looking DNS host, and clean of IP-grabber hosts (the
// immovable floor). The scheme/denylist halves are RE-CHECKED at fetch time
// by gossip — see FetchHostAllowed for why the denylist runs twice.
func FetchURL(raw string) error {
	if len(raw) == 0 || len(raw) > MaxFetchURLLength {
		return ErrFetchURL
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		// Opaque URLs ("https:example.com") have no dialable host; refuse
		// rather than let the fetch-time gate discover it later.
		return ErrFetchURL
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return ErrFetchURL
	}
	if err := FetchHostAllowed(parsed.Hostname()); err != nil {
		return err
	}
	return FloorClean(raw)
}

// FetchHostAllowed is the host half of the SSRF gate, shared by the save-time
// check above AND gossip's pre-dial re-check. It rejects bare IP literals
// (net.ParseIP catches v4 and v6, killing 127.0.0.1 and 169.254.169.254 in
// their literal forms), localhost, and .local/.internal mDNS-style suffixes.
//
// Why the two layers: save-time-only misses hosts that ROT after the save —
// a DNS rebinding, or an entry added to the denylist (or the grabber floor)
// after the def was authored, since stored defs are never re-validated on
// unrelated edits. Fetch-time-only would leave the author discovering the
// refusal at runtime instead of in the editor. Both cost microseconds; the
// pair is cheaper than either failure mode.
func FetchHostAllowed(host string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return ErrFetchHost
	}
	if net.ParseIP(host) != nil {
		return ErrFetchHost
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return ErrFetchHost
	}
	return nil
}

// FetchPath validates optional dot-path extraction segments: each segment
// [A-Za-z0-9_-]+ (indices arrive as bare digits), depth ≤8, empty means the
// definition returns the whole body as text.
func FetchPath(segments []string) error {
	if len(segments) > maxFetchPathDepth {
		return ErrFetchPath
	}
	for _, seg := range segments {
		if !validPathSegment(seg) {
			return ErrFetchPath
		}
	}
	return nil
}

// validPathSegment reports whether one dot-path segment matches
// [A-Za-z0-9_-]+.
func validPathSegment(seg string) bool {
	if len(seg) == 0 {
		return false
	}
	for i := 0; i < len(seg); i++ {
		c := seg[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		default:
			if c != '_' && c != '-' {
				return false
			}
		}
	}
	return true
}

// KeyLabel validates the author-chosen label of a sealed API key: 1-32
// printable ASCII characters (no control characters — the label lands in the
// AAD string, the unique index and audit lines).
func KeyLabel(label string) error {
	if len(label) == 0 || len(label) > MaxKeyLabelLength {
		return ErrKeyLabel
	}
	for i := 0; i < len(label); i++ {
		if label[i] <= ' ' || label[i] > '~' {
			return ErrKeyLabel
		}
	}
	return nil
}

// KeyValue validates the plaintext secret itself: 1-512 bytes. Content is
// otherwise unconstrained — API keys come in every alphabet.
func KeyValue(value string) error {
	if len(value) == 0 || len(value) > MaxKeyValueLength {
		return ErrKeyValue
	}
	return nil
}

// FetchDefQuotaError renders the per-broadcaster cap refusal with the limit
// spelled out, matching how the dashboard surfaces validation errors.
func FetchDefQuotaError() error {
	return fmt.Errorf("fetch definition limit reached (%d per broadcaster)", MaxFetchDefsPerBroadcaster)
}
