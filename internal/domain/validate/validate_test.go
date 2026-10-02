// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package validate_test

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/internal/moderation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	validate.CheckFloor = moderation.CheckFloor
}

func leetify(term string) string {
	return strings.NewReplacer("a", "4", "e", "3", "i", "1", "o", "0", "s", "5").Replace(term)
}

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

func TestBumpCounter(t *testing.T) {
	assert.NoError(t, validate.BumpCounter(""), "empty means no bump, unlike a command name")
	assert.NoError(t, validate.BumpCounter("deaths"))

	assert.Error(t, validate.BumpCounter("has space"))
	assert.Error(t, validate.BumpCounter(validate.CounterName(strings.Repeat("a", 65))))
	assert.Error(t, validate.BumpCounter("target:deaths"))
	assert.Error(t, validate.BumpCounter("bot:feeds"))
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

func TestContentFloor(t *testing.T) {
	terms := moderation.EmbeddedLexicon().Terms(moderation.CatHate)
	require.NotEmpty(t, terms, "embedded hate list empty")
	slur := terms[0]
	cases := []struct {
		name    string
		check   func() error
		refused bool
	}{
		{"slur in a command response", func() error { return validate.CommandResponse("welcome to the stream " + slur) }, true},
		{"obfuscated slur in a command response", func() error { return validate.CommandResponse("hello " + leetify(slur) + " world") }, true},
		{"ip grabber host in a command response", func() error { return validate.CommandResponse("check my setup at grabify.link/pc") }, true},
		{"mild profanity in a command response", func() error { return validate.CommandResponse("that was some bullshit, hell of a play though") }, false},
		{"prize giveaway wording in a command response", func() error { return validate.CommandResponse("type !prize to claim your prize in tonight's giveaway") }, false},
		{"swearing about the game in a command response", func() error { return validate.CommandResponse("damn this fucking game is hard") }, false},
		{"slur in a config string", func() error { return validate.ConfigsJSON([]byte(`{"message":"raid hype ` + slur + ` welcome"}`)) }, true},
		{"slur nested in a config", func() error { return validate.ConfigsJSON([]byte(`{"a":{"b":["fine","also fine","` + slur + `"]}}`)) }, true},
		{"clean config", func() error {
			return validate.ConfigsJSON([]byte(`{"message":"huge shoutout to {raider}, damn what a raid!","count":3}`))
		}, false},
		{"obfuscated slur in a fetch definition name", func() error { return validate.FetchDefName("chat_" + leetify(slur)) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.check()
			if tc.refused {
				assert.ErrorIs(t, err, validate.ErrContentFloor)
				return
			}
			assert.NoError(t, err)
		})
	}
}
