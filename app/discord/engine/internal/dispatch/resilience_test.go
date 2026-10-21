// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package dispatch_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/registry"
	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func observedHarness(t *testing.T, handlers ...module.Handler) (*harness, *observer.ObservedLogs) {
	t.Helper()
	h := newHarness(t, ddiscord.Config{GuildID: testGuild})
	core, logs := observer.New(zapcore.DebugLevel)
	mods := make([]module.Module, len(handlers))
	for i, fn := range handlers {
		mods[i] = module.Module{Name: ddiscord.ModuleVoice, Events: map[string]module.Handler{"GUILD_MEMBER_ADD": fn}}
	}
	h.d.Registry = registry.New(mods...)
	h.d.Log = zap.New(core)
	return h, logs
}

func TestAPanickingHandlerDoesNotStopItsSiblings(t *testing.T) {
	ran := false
	h, logs := observedHarness(t,
		func(context.Context, *module.Context, module.Emit) error { panic("boom") },
		func(context.Context, *module.Context, module.Emit) error { ran = true; return nil },
	)

	h.send("GUILD_MEMBER_ADD", memberPayload(testGuild))

	require.True(t, ran)
	panics := logs.FilterLevelExact(zapcore.ErrorLevel).All()
	require.Len(t, panics, 1)
	require.Equal(t, "boom", panics[0].ContextMap()["panic"])
	require.Equal(t, testGuild, panics[0].ContextMap()["guild_id"])
}

func TestAHandlerFailureLogCarriesTheDeliveryIdentity(t *testing.T) {
	h, logs := observedHarness(t,
		func(context.Context, *module.Context, module.Emit) error { return errors.New("nope") },
	)
	raw := mustMarshal(t, ddiscord.Event{Type: "GUILD_MEMBER_ADD", GuildID: testGuild})
	msg := bus.NewMessage("msg-7", raw)

	require.NoError(t, h.d.Handle(msg))

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	require.Len(t, warns, 1)
	fields := warns[0].ContextMap()
	require.Equal(t, testGuild, fields["guild_id"])
	require.Equal(t, "GUILD_MEMBER_ADD", fields["event_type"])
	require.Equal(t, "msg-7", fields["message_id"])
}

func TestAMalformedEventIsDroppedWithItsMessageID(t *testing.T) {
	h, logs := observedHarness(t)

	require.NoError(t, h.d.Handle(bus.NewMessage("msg-9", []byte{0xff, 0x00})))

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	require.Len(t, warns, 1)
	require.Equal(t, "msg-9", warns[0].ContextMap()["message_id"])
	require.Contains(t, warns[0].ContextMap(), "error")
}

func mustMarshal(t *testing.T, ev ddiscord.Event) []byte {
	t.Helper()
	raw, err := codec.Marshal(ev)
	require.NoError(t, err)
	return raw
}
