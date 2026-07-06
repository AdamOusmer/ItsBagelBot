package validate_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/validate"

	"github.com/stretchr/testify/assert"
)

func TestUserID(t *testing.T) {
	assert.NoError(t, validate.UserID(1))
	assert.ErrorIs(t, validate.UserID(0), validate.ErrUserIDZero)
}

func TestUsername(t *testing.T) {
	assert.NoError(t, validate.Username("Mavey_123"))

	assert.Error(t, validate.Username(""))
	assert.Error(t, validate.Username(strings.Repeat("a", 26)))
	assert.Error(t, validate.Username("with space"))
	assert.Error(t, validate.Username("emoji🚀"))
	assert.Error(t, validate.Username("semi;colon"))
}

func TestEmail(t *testing.T) {
	assert.NoError(t, validate.Email("mavey@concordia.ca"))

	assert.Error(t, validate.Email(""))
	assert.Error(t, validate.Email("not-an-email"))
	assert.Error(t, validate.Email("Display Name <smuggled@evil.com>"), "display names must be refused")
	assert.Error(t, validate.Email("a@b.com\r\nBcc: evil@evil.com"), "header injection must be refused")
}

func TestCommandName(t *testing.T) {
	assert.NoError(t, validate.CommandName("!hello"))
	assert.NoError(t, validate.CommandName("hydrate"))

	assert.Error(t, validate.CommandName(""))
	assert.Error(t, validate.CommandName("has space"))
	assert.Error(t, validate.CommandName("new\nline"))
	assert.Error(t, validate.CommandName("ünïcode"))
	assert.Error(t, validate.CommandName(strings.Repeat("a", 65)))
}

func TestCommandResponse(t *testing.T) {
	assert.NoError(t, validate.CommandResponse("Welcome to the stream! 🎉"))
	assert.NoError(t, validate.CommandResponse("line one\nline two"), "newlines separate chat messages")
	assert.NoError(t, validate.CommandResponse(strings.Repeat("one\n", 4)+"five"), "five lines is the ceiling")
	assert.NoError(t, validate.CommandResponse(strings.Repeat("a", 500)+"\n"+strings.Repeat("b", 500)), "each line gets the full single-message budget")

	assert.Error(t, validate.CommandResponse(""))
	assert.Error(t, validate.CommandResponse("tab\tcharacter"), "control characters must be refused")
	assert.Error(t, validate.CommandResponse("carriage\rreturn"), "CR must be normalized away before validation")
	assert.Error(t, validate.CommandResponse(strings.Repeat("a", 501)), "a single line beyond the message limit")
	assert.Error(t, validate.CommandResponse("ok\n"+strings.Repeat("a", 501)), "any line beyond the message limit")
	assert.Error(t, validate.CommandResponse(strings.Repeat("line\n", 5)+"six"), "more than five lines")
	assert.Error(t, validate.CommandResponse("blank\n\nline"), "blank lines must be normalized away before validation")
}

func TestModuleName(t *testing.T) {
	assert.NoError(t, validate.ModuleName("welcome-bot_2"))

	assert.Error(t, validate.ModuleName(""))
	assert.Error(t, validate.ModuleName("UpperCase"))
	assert.Error(t, validate.ModuleName("evil:enabled"), "a colon could forge another Valkey hash field")
	assert.Error(t, validate.ModuleName(strings.Repeat("a", 65)))
}

func TestConfigsJSON(t *testing.T) {
	assert.NoError(t, validate.ConfigsJSON(nil), "configs are optional")
	assert.NoError(t, validate.ConfigsJSON([]byte(`{"interval":30}`)))

	assert.Error(t, validate.ConfigsJSON([]byte(`{not json`)))

	huge := []byte(`"` + strings.Repeat("a", 17<<10) + `"`)
	assert.Error(t, validate.ConfigsJSON(huge), "oversized configs must be refused")
}

func TestToken(t *testing.T) {
	assert.NoError(t, validate.Token([]byte("oauth-token")))

	assert.Error(t, validate.Token(nil))
	assert.Error(t, validate.Token(make([]byte, 9<<10)))
}

func TestStatus(t *testing.T) {
	assert.NoError(t, validate.Status("free"))
	assert.NoError(t, validate.Status("paid"))
	assert.NoError(t, validate.Status("vip"))

	assert.Error(t, validate.Status("premium"))
	assert.Error(t, validate.Status(""))
}
