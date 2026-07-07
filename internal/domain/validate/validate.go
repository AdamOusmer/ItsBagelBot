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
	"fmt"
	"net/mail"
	"strings"

	"ItsBagelBot/internal/moderation"
)

const (
	maxUsernameLength     = 25  // Twitch login limit
	maxEmailLength        = 254 // RFC 5321
	maxCommandNameLength  = 64
	maxCommandAliases     = 25
	maxResponseLineLength = 500   // Twitch chat message limit, per line
	maxCooldownSeconds    = 86400 // one day; guards against absurd values
	maxModuleNameLength   = 64
	maxConfigsBytes       = 16 << 10
	maxTokenBytes         = 8 << 10
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
	// ErrContentFloor rejects text carrying immovable-floor content (identity
	// slurs, IP-grabber hosts). The bot would post this text as itself, so it is
	// refused at save time regardless of any per-channel automod setting.
	ErrContentFloor = errors.New("text contains a disallowed term (hate or abuse infrastructure)")
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

	// The name is displayed on the dashboard and echoed in chat with every use
	// ("!<name>"), so it is held to the same immovable floor as the response;
	// CommandAliases inherits this per alias. Leet obfuscation folds onto the
	// plain spelling before matching.
	return FloorClean(name)
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
			// A floor hit carries its own precise message; do not blur it
			// into the generic alias error.
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

	// The bot posts this text as itself, so the immovable floor (identity
	// slurs, IP-grabber hosts) is enforced at save time: hosting it in a
	// command risks the broadcaster's channel and the bot account platform-wide
	// (Twitch ToS). Everything milder is deliberately allowed - broadcasters
	// say what they want; only hate and abuse infrastructure are refused.
	return FloorClean(response)
}

// FloorClean rejects text carrying immovable-floor content. Matching runs over
// the normalized skeleton, so leet and lookalike-letter obfuscation folds onto
// the plain spelling.
func FloorClean(text string) error {
	if term, hit := moderation.CheckFloor(text); hit {
		return fmt.Errorf("%w: %q", ErrContentFloor, term)
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

	// Module config strings can end up in bot-emitted chat (a shoutout
	// template, a greeting), so every string value in the blob is held to the
	// same immovable floor as a command response. Keys and non-string values
	// carry no free text. Nested shapes are walked; the 16KiB cap above bounds
	// the work.
	var doc any
	if err := json.Unmarshal(configs, &doc); err != nil {
		return ErrConfigsInvalid
	}
	return floorCleanValues(doc)
}

// floorCleanValues walks a decoded JSON value and floor-checks every string.
func floorCleanValues(v any) error {
	switch t := v.(type) {
	case string:
		return FloorClean(t)
	case map[string]any:
		for _, e := range t {
			if err := floorCleanValues(e); err != nil {
				return err
			}
		}
	case []any:
		for _, e := range t {
			if err := floorCleanValues(e); err != nil {
				return err
			}
		}
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
