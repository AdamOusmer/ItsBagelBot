// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modulevars_test

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/modulevars"
	"ItsBagelBot/pkg/codec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceTemplateConvertsOnlyKnownModuleFields(t *testing.T) {
	fields := []string{"player", "tier", "rr"}
	for _, tc := range []struct{ name, input, want string }{
		{"matching fields", "{player} is {tier} with {rr} RR", "{valorant:player} is {valorant:tier} with {valorant:rr} RR"},
		{"case insensitive", "{PLAYER} {TiEr}", "{valorant:player} {valorant:tier}"},
		{"fallback", "{tier|No competitive rank} {rr|?}", "{valorant:tier|No competitive rank} {valorant:rr|?}"},
		{"unknown and foreign tokens", "{user} {custom} {mcsr:elo} {valorant:rank:tier} {valorant:tier}", "{user} {custom} {mcsr:elo} {valorant:rank:tier} {valorant:tier}"},
		{"plain prose and incomplete tokens", "tier rr PLAYER {tier and {rr", "tier rr PLAYER {tier and {rr"},
		{"condition equality", "{if:tier=Immortal 2:ranked:unranked}", "{if:valorant:tier=Immortal 2:ranked:unranked}"},
		{"condition emptiness", "{if:rr:ranked:unranked}", "{if:valorant:rr:ranked:unranked}"},
		{"single branch condition", "{if:tier:ranked}", "{if:valorant:tier:ranked:}"},
		{"condition literal branches", "{if:tier:ranked {player}:unranked}", "{if:tier:ranked {player}:unranked}"},
		{"unknown condition", "{if:user:yes:no}", "{if:user:yes:no}"},
		{"qualified condition", "{if:valorant:rank:tier=Immortal 2:yes:no}", "{if:valorant:rank:tier=Immortal 2:yes:no}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := modulevars.NamespaceTemplate("valorant", fields, tc.input)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, got, modulevars.NamespaceTemplate("valorant", fields, got), "migration must be idempotent")
		})
	}
}

func TestNamespaceTemplateKeepsRenderedConditionAndFallbackBehavior(t *testing.T) {
	fields := []string{"tier", "rr"}
	for _, template := range []string{
		"{if:tier:ranked}", "{if:tier=Immortal 2:high rank}",
		"{if:tier:ranked:unranked}", "{tier|No rank} ({rr|No RR})",
		"{if:rr:ranked|No RR}",
		"{if:tier:ranked {player}:unranked}",
	} {
		for _, palette := range []module.StringPalette{{"tier": "Immortal 2", "rr": "67"}, {"tier": "", "rr": ""}} {
			migrated := modulevars.NamespaceTemplate("valorant", fields, template)
			assert.Equal(t, palette.Expand(template), palette.ExpandNamespaced("valorant", migrated), template)
		}
	}
}

func TestMigrateConfigChangesOnlyKnownTemplateKeys(t *testing.T) {
	raw := []byte(`{"account":"{player}","rankMessage":"{player}: {tier|unavailable}, {rr} RR","rankEnabled":"off","matchesMessage":"{matches}","unknownMessage":"{tier}","other":{"rankMessage":"{tier}"}}`)
	patch, err := modulevars.MigrateConfig("valorant", raw)
	require.NoError(t, err)
	require.Len(t, patch, 2)
	var rank, matches string
	require.NoError(t, codec.Unmarshal(patch["rankMessage"], &rank))
	require.NoError(t, codec.Unmarshal(patch["matchesMessage"], &matches))
	assert.Equal(t, "{valorant:player}: {valorant:tier|unavailable}, {valorant:rr} RR", rank)
	assert.Equal(t, "{valorant:matches}", matches)
	for _, key := range []string{"account", "rankEnabled", "unknownMessage", "other"} {
		assert.NotContains(t, patch, key)
	}
	second, err := modulevars.MigrateConfig("valorant", []byte(`{"rankMessage":"{valorant:tier}","rankEnabled":"off"}`))
	require.NoError(t, err)
	assert.Empty(t, second)
}

func TestMigrateConfigLeavesUnrelatedOrUnchangedConfigEmpty(t *testing.T) {
	for _, tc := range []struct{ module, config string }{
		{"valorant", ""},
		{"valorant", " \n\t "},
		{"valorant", `null`},
		{"valorant", `{}`},
		{"valorant", `{"rankMessage":"just prose"}`},
		{"valorant", `{"rankMessage":"{valorant:tier} {custom}"}`},
		{"unknown", `{"rankMessage":"{tier}"}`},
		{"triggers", `{"rules":"hello => {triggers:user}"}`},
	} {
		patch, err := modulevars.MigrateConfig(tc.module, []byte(tc.config))
		require.NoError(t, err)
		assert.Empty(t, patch)
	}
}

func TestMigrateConfigNestedBindingsPreserveOtherValues(t *testing.T) {
	for _, tc := range []struct{ name, input, want, key string }{
		{"govee", `{"bindings":[{"rewardId":"r1","replyMessage":"{user}: {color}","allowOff":true},{"rewardId":"r2","replyMessage":"already {govee:color}"}],"device":"{color}"}`, `[{"rewardId":"r1","replyMessage":"{govee:user}: {govee:color}","allowOff":true},{"rewardId":"r2","replyMessage":"already {govee:color}"}]`, "bindings"},
		{"channelpoints", `{"rewards":[{"id":"r1","message":"{user} redeemed {reward} for {cost}","points":100,"onRedeem":"FULFILLED"}],"other":"{user}"}`, `[{"id":"r1","message":"{channelpoints:user} redeemed {channelpoints:reward} for {channelpoints:cost}","points":100,"onRedeem":"FULFILLED"}]`, "rewards"},
		{"songqueue", `{"redeem":{"enabled":true,"rewardId":"r1","replyMessage":"{user} queued {track} from {input} at {pos}; {title} by {artist}","onRedeem":"FULFILLED"},"maxDepth":10}`, `{"enabled":true,"rewardId":"r1","replyMessage":"{songqueue:user} queued {songqueue:track} from {songqueue:input} at {songqueue:pos}; {title} by {artist}","onRedeem":"FULFILLED"}`, "redeem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patch, err := modulevars.MigrateConfig(tc.name, []byte(tc.input))
			require.NoError(t, err)
			require.Len(t, patch, 1)
			assert.JSONEq(t, tc.want, string(patch[tc.key]))
			merged := map[string]codec.RawMessage{}
			require.NoError(t, codec.Unmarshal([]byte(tc.input), &merged))
			merged[tc.key] = patch[tc.key]
			raw, err := codec.Marshal(merged)
			require.NoError(t, err)
			second, err := modulevars.MigrateConfig(tc.name, raw)
			require.NoError(t, err)
			assert.Empty(t, second, "nested migration must be idempotent")
		})
	}
}

func TestMigrateConfigLegacySingleGoveeBinding(t *testing.T) {
	patch, err := modulevars.MigrateConfig("govee", []byte(`{"rewardId":"r1","device":"lamp","replyMessage":"{user} picked {color}"}`))
	require.NoError(t, err)
	require.Len(t, patch, 1)
	assert.JSONEq(t, `"{govee:user} picked {govee:color}"`, string(patch["replyMessage"]))
}

func TestMigrateConfigStructuredTriggerResponses(t *testing.T) {
	rules := `[{"phrase":"hello {user}","response":"hi {user} from {channel}","match":"exact","enabled":false},{"phrase":"bye","response":"bye {triggers:user}"}]`
	config, err := codec.Marshal(map[string]any{"rules": rules, "unrelated": "{user}"})
	require.NoError(t, err)
	patch, err := modulevars.MigrateConfig("triggers", config)
	require.NoError(t, err)
	require.Len(t, patch, 1)
	var got string
	require.NoError(t, codec.Unmarshal(patch["rules"], &got))
	assert.JSONEq(t, `[{"phrase":"hello {user}","response":"hi {triggers:user} from {triggers:channel}","match":"exact","enabled":false},{"phrase":"bye","response":"bye {triggers:user}"}]`, got)
}

func TestMigrateConfigLegacyTriggerResponses(t *testing.T) {
	config, err := codec.Marshal(map[string]string{"rules": "# greeting\nhello {user} => hi {user}\ncontains: lol => {channel} laughs\n"})
	require.NoError(t, err)
	patch, err := modulevars.MigrateConfig("triggers", config)
	require.NoError(t, err)
	require.Len(t, patch, 1)
	var got string
	require.NoError(t, codec.Unmarshal(patch["rules"], &got))
	assert.Equal(t, "# greeting\nhello {user} => hi {triggers:user}\ncontains: lol => {triggers:channel} laughs\n", got)
}

func TestMigrateConfigRejectsInvalidJSON(t *testing.T) {
	_, err := modulevars.MigrateConfig("valorant", []byte(`{"rankMessage":`))
	assert.Error(t, err)
}

func TestMigrateConfigLegacySongqueueChatTemplatesUseTheirExactPalettes(t *testing.T) {
	raw := []byte(`{"addMessage":"{user}: {title} by {artist} #{pos}; {url} {req} {unknown}","playingMessage":"{user}: {title} by {artist} for {req}; {pos} {url}","retractMessage":"{user} removed {title}; {artist} {pos}","currentMessage":"{user}: {title} by {artist} {url} for {req}; {pos}","device":"{title}","unknownMessage":"{user}"}`)
	patch, err := modulevars.MigrateConfig("songqueue", raw)
	require.NoError(t, err)
	require.Len(t, patch, 4)
	for key, want := range map[string]string{
		"addMessage":     `{songqueue:user}: {songqueue:title} by {songqueue:artist} #{songqueue:pos}; {url} {req} {unknown}`,
		"playingMessage": `{songqueue:user}: {songqueue:title} by {songqueue:artist} for {songqueue:req}; {pos} {url}`,
		"retractMessage": `{songqueue:user} removed {songqueue:title}; {artist} {pos}`,
		"currentMessage": `{songqueue:user}: {songqueue:title} by {songqueue:artist} {songqueue:url} for {songqueue:req}; {pos}`,
	} {
		var got string
		require.NoError(t, codec.Unmarshal(patch[key], &got))
		assert.Equal(t, want, got)
	}
	assert.NotContains(t, patch, "device")
	assert.NotContains(t, patch, "unknownMessage")
	merged := map[string]codec.RawMessage{}
	require.NoError(t, codec.Unmarshal(raw, &merged))
	for key, value := range patch {
		merged[key] = value
	}
	converted, err := codec.Marshal(merged)
	require.NoError(t, err)
	second, err := modulevars.MigrateConfig("songqueue", converted)
	require.NoError(t, err)
	assert.Empty(t, second)
}

func TestMigrateConfigLegacyQueueLifecycleTemplatesOnlyOwnUser(t *testing.T) {
	raw := []byte(`{"openedMessage":"{user} opened queue; {count} {position} {option}","closedMessage":"{user} closed queue; {list} {queue:user}","openCommand":"{user}","unknownMessage":"{user}"}`)
	patch, err := modulevars.MigrateConfig("queue", raw)
	require.NoError(t, err)
	require.Len(t, patch, 2)
	assert.JSONEq(t, `"{queue:user} opened queue; {count} {position} {option}"`, string(patch["openedMessage"]))
	assert.JSONEq(t, `"{queue:user} closed queue; {list} {queue:user}"`, string(patch["closedMessage"]))
	assert.NotContains(t, patch, "openCommand")
	assert.NotContains(t, patch, "unknownMessage")
}
