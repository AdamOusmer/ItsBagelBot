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

	assert.Error(t, validate.CommandResponse(""))
	assert.Error(t, validate.CommandResponse("line\nbreak"), "control characters must be refused")
	assert.Error(t, validate.CommandResponse(strings.Repeat("a", 501)))
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

func TestTransactionID(t *testing.T) {
	assert.NoError(t, validate.TransactionID("tbx-20260609-AB12"))

	assert.Error(t, validate.TransactionID(""))
	assert.Error(t, validate.TransactionID("has space"))
	assert.Error(t, validate.TransactionID("semi;colon"))
	assert.Error(t, validate.TransactionID(strings.Repeat("a", 65)))
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
