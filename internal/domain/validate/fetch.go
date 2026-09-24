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

const (
	MaxFetchDefsPerBroadcaster = 20

	MaxFetchURLLength = 512

	maxFetchPathDepth = 8

	MaxKeyLabelLength = 32
	MaxKeyValueLength = 512
)

var (
	ErrFetchDefName = errors.New("fetch name must be 1-32 characters of [a-z0-9_]")
	ErrFetchURL     = errors.New("fetch url must be an absolute https url of at most 512 characters")
	ErrFetchHost    = errors.New("fetch url host must be a public dns name (no ip literals, localhost, .local or .internal)")
	ErrFetchPath    = errors.New("json path segments must be [A-Za-z0-9_-], at most 8 deep")
	ErrKeyLabel     = errors.New("key label must be 1-32 printable ascii characters")
	ErrKeyValue     = errors.New("key value must be 1-512 characters")
)

func FetchDefName(name string) error {
	if len(name) == 0 || len(name) > 32 {
		return ErrFetchDefName
	}
	if !isFetchNameCharset(name) {
		return ErrFetchDefName
	}
	return FloorClean(strings.ReplaceAll(name, "_", " "))
}

func isFetchNameCharset(name string) bool {
	for i := 0; i < len(name); i++ {
		if !isFetchNameByte(name[i]) {
			return false
		}
	}
	return true
}

func isFetchNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	return c == '_'
}

func FetchURL(raw string) error {
	if len(raw) == 0 || len(raw) > MaxFetchURLLength {
		return ErrFetchURL
	}

	parsed := parsedAbsoluteHTTPS(raw)
	if parsed == nil {
		return ErrFetchURL
	}
	if err := FetchHostAllowed(parsed.Hostname()); err != nil {
		return err
	}
	return FloorClean(raw)
}

func parsedAbsoluteHTTPS(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		return nil
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return nil
	}
	return parsed
}

// Gossip must re-run this before every dial: the denylist can change after save.
func FetchHostAllowed(host string) error {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	switch {
	case host == "":
		return ErrFetchHost
	case net.ParseIP(host) != nil:
		return ErrFetchHost
	case deniedHostName(host):
		return ErrFetchHost
	}
	return nil
}

func deniedHostName(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal")
}

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

func validPathSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for i := 0; i < len(seg); i++ {
		switch c := seg[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

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

func KeyValue(value string) error {
	if len(value) == 0 || len(value) > MaxKeyValueLength {
		return ErrKeyValue
	}
	return nil
}

func FetchDefQuotaError() error {
	return fmt.Errorf("fetch definition limit reached (%d per broadcaster)", MaxFetchDefsPerBroadcaster)
}
