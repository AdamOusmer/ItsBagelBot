// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation_test

import (
	"testing"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCooldownAddSecondsFlatAndRated(t *testing.T) {
	cases := []struct {
		name   string
		p      automation.CooldownAddAction
		amount int64
		want   int64
		ok     bool
	}{
		{"flat add ignores amount", automation.CooldownAddAction{Seconds: 30}, 0, 30, true},
		{"one lot at the rate", automation.CooldownAddAction{Seconds: 30, Per: 5}, 5, 30, true},
		{"several lots, no remainder carried", automation.CooldownAddAction{Seconds: 30, Per: 5}, 12, 60, true},
		{"below the rate adds nothing", automation.CooldownAddAction{Seconds: 30, Per: 5}, 4, 0, false},
		{"zero amount below the rate", automation.CooldownAddAction{Seconds: 30, Per: 5}, 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := automation.CooldownAddSeconds(c.p, c.amount)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestCooldownAddKeyIsLowercasedAndPrefixed(t *testing.T) {
	key := automation.LongCooldownNamedKey(42, "Subathon")
	assert.Equal(t, "cooldown:named:42:subathon", key)
	assert.True(t, automation.IsNamedCooldownKey(key))
	assert.False(t, automation.IsNamedCooldownKey("cooldown:auto:42:id"))
}

func cooldownAddDef(t *testing.T, trigger, params string) automation.Definition {
	t.Helper()
	raw := `{"version":1,"trigger":` + trigger + `,"actions":[{"type":"cooldown_add","params":` + params + `}]}`
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(raw, &def))
	return def
}

func TestCooldownAddValidation(t *testing.T) {
	eventTrigger := `{"type":"event","params":{"kind":"channel.subscription.gift"}}`
	manualTrigger := `{"type":"manual","params":{}}`

	t.Run("per without a numeric amount trigger is a mismatch", func(t *testing.T) {
		def := cooldownAddDef(t, manualTrigger, `{"name":"subathon","seconds":30,"per":1}`)
		issues := automation.Validate(def)
		require.NotEmpty(t, issues)
		found := false
		for _, i := range issues {
			if i.Code == automation.CodeTriggerMismatch {
				found = true
			}
		}
		assert.True(t, found, "%v", issues)
	})

	t.Run("per with an event trigger that carries amount validates", func(t *testing.T) {
		def := cooldownAddDef(t, eventTrigger, `{"name":"subathon","seconds":30,"per":1}`)
		assert.Empty(t, automation.Validate(def))
	})

	t.Run("flat add needs no amount trigger", func(t *testing.T) {
		def := cooldownAddDef(t, manualTrigger, `{"name":"subathon","seconds":30}`)
		assert.Empty(t, automation.Validate(def))
	})

	t.Run("a streamer-typed name must be one lowercase word", func(t *testing.T) {
		def := cooldownAddDef(t, manualTrigger, `{"name":"Sub-athon","seconds":30}`)
		assert.NotEmpty(t, automation.Validate(def))
	})
}

func cooldownCondDef(t *testing.T, params string) automation.Definition {
	t.Helper()
	raw := `{"version":1,"trigger":{"type":"manual","params":{}},"conditions":[{"type":"cooldown","params":` + params + `}]}`
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(raw, &def))
	return def
}

func TestNamedCooldownConditionValidation(t *testing.T) {
	t.Run("a name needs global_s", func(t *testing.T) {
		issues := automation.Validate(cooldownCondDef(t, `{"user_s":60,"name":"subathon"}`))
		assert.NotEmpty(t, issues)
	})
	t.Run("a name with global_s validates", func(t *testing.T) {
		assert.Empty(t, automation.Validate(cooldownCondDef(t, `{"global_s":60,"name":"subathon"}`)))
	})
	t.Run("a name cannot also claim a per-viewer window", func(t *testing.T) {
		issues := automation.Validate(cooldownCondDef(t, `{"global_s":60,"user_s":60,"name":"subathon"}`))
		assert.NotEmpty(t, issues)
	})
}

func TestOwnedCooldownNamesFindsTopLevelAndNestedOwners(t *testing.T) {
	raw := `{"version":1,"trigger":{"type":"manual","params":{}},
"conditions":[{"type":"cooldown","params":{"global_s":60,"name":"subathon"}}],
"actions":[{"type":"if","params":{"condition":{"type":"cooldown","params":{"global_s":60,"name":"nested"}},"then":[],"else":[]}}]}`
	var def automation.Definition
	require.NoError(t, codec.UnmarshalFromString(raw, &def))
	require.Empty(t, automation.Validate(def))
	assert.ElementsMatch(t, []string{"subathon", "nested"}, automation.OwnedCooldownNames(def))
}
