// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// client_id and state never reach Users: the contract keeps both as
// Dashboard-owned HTTP input, so no validator for them lives here.
const (
	maxDeviceNameLength = 64
	minVerifierLength   = 43
	maxVerifierLength   = 128
	challengeLength     = 43
	minRedirectPort     = 1024
	maxRedirectPort     = 65535
)

var (
	ErrRedirectURI    = errors.New("redirect_uri must be http://127.0.0.1:<port>/callback with no path, query, fragment or userinfo")
	ErrChallenge      = errors.New("code_challenge must be a 43-character S256 value")
	ErrVerifier       = errors.New("code_verifier must be 43 to 128 characters of the RFC 7636 alphabet")
	ErrDevicePlatform = errors.New("platform must be macos, windows or linux")
)

// DeviceRedirectURI rejects anything but http://127.0.0.1:<port>/callback.
func DeviceRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return ErrRedirectURI
	}
	if !isPlainHTTP(u) {
		return ErrRedirectURI
	}
	if !isBareCallback(u) {
		return ErrRedirectURI
	}
	return deviceRedirectHost(u)
}

func isPlainHTTP(u *url.URL) bool {
	return u.Scheme == "http" && u.User == nil
}

func isBareCallback(u *url.URL) bool {
	return u.Path == "/callback" && u.RawQuery == "" && u.Fragment == ""
}

func deviceRedirectHost(u *url.URL) error {
	if u.Hostname() != "127.0.0.1" {
		return ErrRedirectURI
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return ErrRedirectURI
	}
	if !isRedirectPort(port) {
		return ErrRedirectURI
	}
	return nil
}

func isRedirectPort(port int) bool {
	return port >= minRedirectPort && port <= maxRedirectPort
}

// DeviceChallenge checks shape only; PKCEVerify compares the real value.
func DeviceChallenge(challenge string) error {
	if len(challenge) != challengeLength {
		return ErrChallenge
	}
	for i := 0; i < len(challenge); i++ {
		if !isBase64URLByte(challenge[i]) {
			return ErrChallenge
		}
	}
	return nil
}

func DeviceVerifier(verifier string) error {
	if len(verifier) < minVerifierLength || len(verifier) > maxVerifierLength {
		return ErrVerifier
	}
	for i := 0; i < len(verifier); i++ {
		if !isPKCEVerifierByte(verifier[i]) {
			return ErrVerifier
		}
	}
	return nil
}

func isPKCEVerifierByte(c byte) bool {
	return isAlphanumeric(c) || c == '-' || c == '.' || c == '_' || c == '~'
}

func isBase64URLByte(c byte) bool {
	return isAlphanumeric(c) || c == '-' || c == '_'
}

// DevicePlatform constrains to the ent enum's accepted values.
func DevicePlatform(platform string) error {
	switch platform {
	case "macos", "windows", "linux":
		return nil
	default:
		return ErrDevicePlatform
	}
}

// DeviceName strips control and format characters and bounds length; it never
// fails, since the reported name is untrusted and shown on the consent page,
// where a bidi override could reorder what the user reads.
func DeviceName(raw string) string {
	cleaned := strings.TrimSpace(strings.Map(displayableRune, raw))
	return string(firstRunes([]rune(cleaned), maxDeviceNameLength))
}

func displayableRune(r rune) rune {
	if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
		return -1
	}
	return r
}

func firstRunes(runes []rune, limit int) []rune {
	if len(runes) <= limit {
		return runes
	}
	return runes[:limit]
}
