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
)

const (
	maxUsernameLength      = 25  // Twitch login limit
	maxEmailLength         = 254 // RFC 5321
	maxCommandNameLength   = 64
	maxResponseLength      = 500 // Twitch chat message limit
	maxModuleNameLength    = 64
	maxConfigsBytes        = 16 << 10
	maxTransactionIDLength = 64
	maxTokenBytes          = 8 << 10
)

var (
	ErrUserIDZero       = errors.New("user id must not be zero")
	ErrUsernameInvalid  = errors.New("username must be 1-25 characters of [a-zA-Z0-9_]")
	ErrEmailInvalid     = errors.New("email address is not valid")
	ErrCommandName      = errors.New("command name must be 1-64 printable ASCII characters without spaces")
	ErrResponseInvalid  = errors.New("command response must be 1-500 characters without control characters")
	ErrModuleName       = errors.New("module name must be 1-64 characters of [a-z0-9_-]")
	ErrConfigsInvalid   = errors.New("module configs must be valid JSON of at most 16KiB")
	ErrTransactionID    = errors.New("transaction id must be 1-64 characters of [a-zA-Z0-9_-]")
	ErrTokenInvalid     = errors.New("token must be 1 byte to 8KiB")
	ErrStatusInvalid    = errors.New("status must be free, paid or vip")
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

func CommandResponse(response string) error {

	if len(response) == 0 || len(response) > maxResponseLength {
		return ErrResponseInvalid
	}

	for _, r := range response {
		if r < ' ' { // control characters, including CR/LF
			return ErrResponseInvalid
		}
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

func TransactionID(id string) error {

	if len(id) == 0 || len(id) > maxTransactionIDLength {
		return ErrTransactionID
	}

	for i := 0; i < len(id); i++ {
		c := id[i]
		if !isAlphanumeric(c) && c != '_' && c != '-' {
			return ErrTransactionID
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
