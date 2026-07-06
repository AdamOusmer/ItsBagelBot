// Package validate holds the input rules enforced at every trust boundary:
// repository methods fed by RPC or webhooks, and event payloads consumed from
// the bus. ent parameterizes all SQL, so the concerns here are domain
// validity, resource caps (a config blob must not be able to blow up the
// database or Valkey), and key safety (a module name becomes part of a Valkey
// hash field, so its charset is strict).
package validate

import (
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
)

const (
	maxUsernameLength      = 25  // Twitch login limit
	maxEmailLength         = 254 // RFC 5321
	maxCommandNameLength   = 64
	maxCommandAliases      = 25
	maxResponseLineLength  = 500   // Twitch chat message limit, per line
	maxCooldownSeconds     = 86400 // one day; guards against absurd values
	maxModuleNameLength    = 64
	maxConfigsBytes        = 16 << 10
	maxTokenBytes          = 8 << 10
)

// MaxResponseLines caps how many chat messages one command response may fan
// out into: the response is newline-delimited and the bot sends one message
// per line. Exported so the worker can enforce the same ceiling at emit time.
const MaxResponseLines = 5

var (
	ErrUserIDZero      = errors.New("user id must not be zero")
	ErrUsernameInvalid = errors.New("username must be 1-25 characters of [a-zA-Z0-9_]")
	ErrEmailInvalid    = errors.New("email address is not valid")
	ErrCommandName     = errors.New("command name must be 1-64 printable ASCII characters without spaces")
	ErrCommandAliases  = errors.New("aliases must each be a valid command name, unique, and at most 25 in total")
	ErrResponseInvalid = errors.New("command response must be 1-5 lines, each 1-500 characters without control characters")
	ErrPermInvalid     = errors.New("perm must be one of everyone, sub, vip, mod, lead_mod, broadcaster")
	ErrCooldownInvalid = errors.New("cooldown must be between 0 and 86400 seconds")
	ErrModuleName      = errors.New("module name must be 1-64 characters of [a-z0-9_-]")
	ErrConfigsInvalid  = errors.New("module configs must be valid JSON of at most 16KiB")
	ErrTokenInvalid    = errors.New("token must be 1 byte to 8KiB")
	ErrStatusInvalid   = errors.New("status must be free, paid or vip")
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

func Email(email string) error {

	if len(email) == 0 || len(email) > maxEmailLength {
		return ErrEmailInvalid
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		// A mismatch means the input smuggled a display name or comment
		// around the address, which we never accept as an email value.
		return ErrEmailInvalid
	}

	return nil
}

func CommandName(name string) error {

	if len(name) == 0 || len(name) > maxCommandNameLength {
		return ErrCommandName
	}

	for i := 0; i < len(name); i++ {
		// Printable ASCII without space: blocks control characters,
		// whitespace tricks and invisible unicode in command lookups.
		if name[i] <= ' ' || name[i] > '~' {
			return ErrCommandName
		}
	}

	return nil
}

// CommandAliases validates the alternate names a command answers to: each must
// be a valid command name, the set must be free of duplicates (case-insensitive,
// matching the lower-cased lookup the bot does), and the count is capped.
func CommandAliases(aliases []string) error {

	if len(aliases) > maxCommandAliases {
		return ErrCommandAliases
	}

	seen := make(map[string]struct{}, len(aliases))
	for _, alias := range aliases {
		if err := CommandName(alias); err != nil {
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

// CommandResponse checks a newline-delimited command response: at most
// MaxResponseLines lines, each line 1-500 characters with no control
// characters. The bot sends one chat message per line, so each line is held
// to the single-message limit. Callers normalize first (CRLF folded to LF,
// blank lines dropped), so a blank line here is a hard error, not noise.
func CommandResponse(response string) error {

	if len(response) == 0 {
		return ErrResponseInvalid
	}

	lines := strings.Split(response, "\n")
	if len(lines) > MaxResponseLines {
		return ErrResponseInvalid
	}

	for _, line := range lines {
		if len(line) == 0 || len(line) > maxResponseLineLength {
			return ErrResponseInvalid
		}
		for _, r := range line {
			if r < ' ' { // control characters, including CR
				return ErrResponseInvalid
			}
		}
	}

	return nil
}

// Perm is the minimum role tier allowed to run a command. The set mirrors the
// dashboard <select>; an unknown value is rejected rather than silently coerced
// so a typo never widens or narrows access by accident.
func Perm(perm string) error {

	switch perm {
	case "everyone", "sub", "vip", "mod", "lead_mod", "broadcaster":
		return nil
	}

	return ErrPermInvalid
}

// Cooldown caps the per-command cooldown in seconds.
func Cooldown(seconds uint) error {

	if seconds > maxCooldownSeconds {
		return ErrCooldownInvalid
	}

	return nil
}

// ModuleName is strict because the name is embedded into the Valkey hash
// field "module:<name>:enabled"; a ':' here could forge another field.
func ModuleName(name string) error {

	if len(name) == 0 || len(name) > maxModuleNameLength {
		return ErrModuleName
	}

	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return ErrModuleName
		}
	}

	return nil
}

func ConfigsJSON(configs []byte) error {

	if len(configs) == 0 {
		return nil // absent configs are fine, modules can be pure toggles
	}

	if len(configs) > maxConfigsBytes || !json.Valid(configs) {
		return ErrConfigsInvalid
	}

	return nil
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
