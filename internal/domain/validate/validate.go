// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate

import (
	"ItsBagelBot/pkg/codec"
	"errors"
	"fmt"
	"net/mail"
	"strings"
)

const (
	maxUsernameLength    = 25
	maxEmailLength       = 254
	maxCommandNameLength = 64
	maxCommandAliases    = 25
	maxCooldownSeconds   = 86400
	maxModuleNameLength  = 64
	maxConfigsBytes      = 16 << 10
	maxTokenBytes        = 8 << 10
)

const MaxResponseLineLength = 500

const MaxResponseLines = 5

var (
	ErrUserIDZero         = errors.New("user id must not be zero")
	ErrUsernameInvalid    = errors.New("username must be 1-25 characters of [a-zA-Z0-9_]")
	ErrEmailInvalid       = errors.New("email address is not valid")
	ErrCommandName        = errors.New("command name must be 1-64 printable ASCII characters without spaces")
	ErrCommandAliases     = errors.New("aliases must each be a valid command name, unique, and at most 25 in total")
	ErrResponseInvalid    = errors.New("command response must be 1-5 lines, each 1-500 characters without control characters")
	ErrPermInvalid        = errors.New("perm must be one of everyone, sub, vip, mod, lead_mod, broadcaster")
	ErrCooldownInvalid    = errors.New("cooldown must be between 0 and 86400 seconds")
	ErrBumpCounterInvalid = errors.New("bump counter name must be at most 64 printable ASCII characters without spaces or ':'")
	ErrModuleName         = errors.New("module name must be 1-64 characters of [a-z0-9_-]")
	ErrConfigsInvalid     = errors.New("module configs must be valid JSON of at most 16KiB")
	ErrTokenInvalid       = errors.New("token must be 1 byte to 8KiB")
	ErrStatusInvalid      = errors.New("status must be free, paid or vip")
	ErrContentFloor       = errors.New("text contains a disallowed term (hate or abuse infrastructure)")
)

func UserID(id uint64) error {

	if id == 0 {
		return ErrUserIDZero
	}

	return nil
}

func Username(username string) error {

	if len(username) == 0 || len(username) > maxUsernameLength {
		return ErrUsernameInvalid
	}

	for i := 0; i < len(username); i++ {
		c := username[i]
		if !isAlphanumeric(c) && c != '_' {
			return ErrUsernameInvalid
		}
	}

	return nil
}

func isPrintableAsciiNoSpace(c byte) bool {
	return c > ' ' && c <= '~'
}

func Email(email string) error {
	if len(email) == 0 || len(email) > maxEmailLength {
		return ErrEmailInvalid
	}

	parsed, err := mail.ParseAddress(email)
	hasSmuggledCommentOrDisplayName := parsed != nil && parsed.Address != email
	if err != nil || hasSmuggledCommentOrDisplayName {
		return ErrEmailInvalid
	}

	return nil
}

func CommandName(name string) error {
	if len(name) == 0 || len(name) > maxCommandNameLength {
		return ErrCommandName
	}

	for i := 0; i < len(name); i++ {
		if !isPrintableAsciiNoSpace(name[i]) {
			return ErrCommandName
		}
	}

	return FloorClean(name)
}

func CommandAliases(aliases []string) error {

	if len(aliases) > maxCommandAliases {
		return ErrCommandAliases
	}

	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		if err := CommandName(alias); err != nil {
			if errors.Is(err, ErrContentFloor) {
				return err
			}
			return ErrCommandAliases
		}
		key := strings.ToLower(alias)
		if _, dup := seen[key]; dup {
			return ErrCommandAliases
		}
		seen[key] = struct{}{}
	}

	return nil
}

func CommandResponse(response string) error {

	if len(response) == 0 {
		return ErrResponseInvalid
	}

	lines := strings.Split(response, "\n")
	if len(lines) > MaxResponseLines {
		return ErrResponseInvalid
	}

	for _, line := range lines {
		if !validResponseLine(line) {
			return ErrResponseInvalid
		}
	}

	return FloorClean(response)
}

func validResponseLine(line string) bool {
	if len(line) == 0 || len(line) > MaxResponseLineLength {
		return false
	}
	for _, r := range line {
		if r < ' ' {
			return false
		}
	}
	return true
}

var CheckFloor func(text string) (term string, hit bool)

func FloorClean(text string) error {
	if CheckFloor == nil {
		return nil
	}
	if term, hit := CheckFloor(text); hit {
		return fmt.Errorf("%w: %q", ErrContentFloor, term)
	}
	return nil
}

func Perm(perm string) error {

	switch perm {
	case "everyone", "sub", "vip", "mod", "lead_mod", "broadcaster":
		return nil
	}

	return ErrPermInvalid
}

func Cooldown(seconds uint) error {

	if seconds > maxCooldownSeconds {
		return ErrCooldownInvalid
	}

	return nil
}

type CounterName string

func BumpCounter(name CounterName) error {
	if name == "" {
		return nil
	}

	if len(name) > maxCommandNameLength {
		return ErrBumpCounterInvalid
	}

	for i := 0; i < len(name); i++ {
		if !isPrintableAsciiNoSpace(name[i]) || name[i] == ':' {
			return ErrBumpCounterInvalid
		}
	}

	return FloorClean(string(name))
}

func ModuleName(name string) error {

	if len(name) == 0 || len(name) > maxModuleNameLength {
		return ErrModuleName
	}

	for i := 0; i < len(name); i++ {
		if !validModuleChar(name[i]) {
			return ErrModuleName
		}
	}

	return nil
}

func validModuleChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	default:
		return c == '_' || c == '-'
	}
}

func ConfigsJSON(configs []byte) error {

	if len(configs) == 0 {
		return nil
	}

	if len(configs) > maxConfigsBytes || !codec.Valid(configs) {
		return ErrConfigsInvalid
	}

	var doc any
	if err := codec.Unmarshal(configs, &doc); err != nil {
		return ErrConfigsInvalid
	}
	return floorCleanValues(doc)
}

func floorCleanValues(v any) error {
	switch t := v.(type) {
	case string:
		return FloorClean(t)
	case map[string]any:
		return floorCleanEach(mapValues(t))
	case []any:
		return floorCleanEach(t)
	}
	return nil
}

func floorCleanEach(values []any) error {
	for _, e := range values {
		if err := floorCleanValues(e); err != nil {
			return err
		}
	}
	return nil
}

func mapValues(m map[string]any) []any {
	out := make([]any, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func Token(token []byte) error {

	if len(token) == 0 || len(token) > maxTokenBytes {
		return ErrTokenInvalid
	}

	return nil
}

func Status(status string) error {

	switch status {
	case "free", "paid", "vip":
		return nil
	}

	return ErrStatusInvalid
}

func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
