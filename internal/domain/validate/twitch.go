// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"errors"
	"regexp"
	"unicode"
	"unicode/utf8"
)

const maxDisplayNameRunes = 64

var (
	LoginPattern          = regexp.MustCompile(`^[a-z0-9_]{1,25}$`)
	ErrLoginInvalid       = errors.New("login must be 1-25 characters of [a-z0-9_]")
	ErrDisplayNameInvalid = errors.New("display name must be 1-64 printable characters")
)

func Login(login string) error {
	if !LoginPattern.MatchString(login) {
		return ErrLoginInvalid
	}
	return nil
}

func DisplayName(name string) error {
	if !fitsDisplayName(name) {
		return ErrDisplayNameInvalid
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return ErrDisplayNameInvalid
		}
	}
	return nil
}

func fitsDisplayName(name string) bool {
	runes := utf8.RuneCountInString(name)
	return utf8.ValidString(name) && runes > 0 && runes <= maxDisplayNameRunes
}
