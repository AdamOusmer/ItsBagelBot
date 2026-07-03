// Package builtin holds the worker's shipped modules: the baked-primitives
// module (reserved commands + the bagel greeter), the live tracker, and the
// opt-in shoutout module. Each implements module.Module and is registered by the
// worker at startup. Command dispatch and gating live in module.CommandRouter;
// these modules only declare their Commands and fill the Output on Run.
package builtin

import (
	"context"
	"fmt"
	"time"

	workeri18n "ItsBagelBot/app/worker/i18n"
	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

const (
	bagelMessage = "🥯🥯🥯🥯🥯🥯"
	bagelCount   = 3
)

// BakedModule is the hidden, always-on core module of baked primitives. It is
// never listed on the dashboard and cannot be toggled or reconfigured by a
// broadcaster. It owns two things:
//
//   - the reserved baked commands (!ping, !itsbagelbot, !source, !announce*),
//     declared via Commands() and dispatched/gated by the central router; a
//     custom or default command can never shadow these names.
//   - the bagel greeting: a special user's first message on a live stream (any
//     message, command or not) gets three bagels, once per stream.
type BakedModule struct {
	special   *module.SpecialSet
	live      module.IsLiveChecker
	greet     module.GreetStore
	startedAt time.Time
	log       *zap.Logger
}

func NewBakedModule(special *module.SpecialSet, live module.IsLiveChecker, greet module.GreetStore, log *zap.Logger) *BakedModule {
	return &BakedModule{special: special, live: live, greet: greet, startedAt: time.Now(), log: log}
}

func (m *BakedModule) Name() string     { return "" } // core: always on, never listed
func (m *BakedModule) Events() []string { return []string{"channel.chat.message"} }

// Commands declares the reserved baked commands. The router indexes and gates
// them; each Run only fills an Output and emits it. Perms: info commands are
// everyone, announces are moderator.
func (m *BakedModule) Commands() []module.Command {
	announce := func(color string) func(context.Context, *module.Context, string, module.Emit) error {
		return func(_ context.Context, c *module.Context, args string, emit module.Emit) error {
			if args == "" {
				return nil
			}
			emit(&module.Output{
				Type:          outgress.TypeAnnounce,
				BroadcasterID: c.Env.BroadcasterUserID,
				Text:          args,
				Color:         color,
			})
			return nil
		}
	}

	return []module.Command{
		{
			Name: "ping",
			Perm: module.RoleEveryone,
			Run: func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
				emit(&module.Output{
					Type:          outgress.TypeChat,
					BroadcasterID: c.Env.BroadcasterUserID,
					Text:          fmt.Sprintf(workeri18n.T(c.Locale, "ping"), humanizeUptime(time.Since(m.startedAt))),
				})
				return nil
			},
		},
		{
			Name: "itsbagelbot",
			Perm: module.RoleEveryone,
			Run: func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
				emit(&module.Output{
					Type:          outgress.TypeChat,
					BroadcasterID: c.Env.BroadcasterUserID,
					Text:          "ItsBagelBot 🥯 → https://itsbagelbot.com",
				})
				return nil
			},
		},
		{
			Name: "source",
			Perm: module.RoleEveryone,
			Run: func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
				emit(&module.Output{
					Type:          outgress.TypeChat,
					BroadcasterID: c.Env.BroadcasterUserID,
					Text:          "Source → https://github.com/AdamOusmer/ItsBagelBot",
				})
				return nil
			},
		},
		{Name: "announce", Perm: module.RoleModerator, Run: announce("primary")},
		{Name: "announceblue", Perm: module.RoleModerator, Run: announce("blue")},
		{Name: "announcegreen", Perm: module.RoleModerator, Run: announce("green")},
		{Name: "announceorange", Perm: module.RoleModerator, Run: announce("orange")},
		{Name: "announcepurple", Perm: module.RoleModerator, Run: announce("purple")},
	}
}

// Handle runs the bagel greeting on the non-command path: any first message from
// a special user while the stream is live yields three bagels. Errors are logged
// and swallowed so a greet failure never blocks the message.
func (m *BakedModule) Handle(ctx context.Context, c *module.Context, emit module.Emit) error {
	if !m.special.Has(c.Env.ChatterUserID) {
		return nil
	}

	live, err := m.live.IsLive(ctx, c.BroadcasterID)
	if err != nil {
		m.log.Warn("baked: live check failed for bagel", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return nil
	}
	if !live {
		return nil
	}

	first, err := m.greet.FirstGreet(ctx, c.BroadcasterID, c.Env.ChatterUserID)
	if err != nil {
		m.log.Warn("baked: greet check failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return nil
	}
	if !first {
		return nil
	}

	m.log.Debug("bagel greet",
		zap.String("chatter_id", c.Env.ChatterUserID),
		zap.Uint64("broadcaster_id", c.BroadcasterID),
	)
	for i := 0; i < bagelCount; i++ {
		emit(&module.Output{
			Type:          outgress.TypeChat,
			BroadcasterID: c.Env.BroadcasterUserID,
			Text:          bagelMessage,
		})
	}
	return nil
}

func humanizeUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	mn := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mn)
	}
	if mn > 0 {
		return fmt.Sprintf("%dm %ds", mn, s)
	}
	return fmt.Sprintf("%ds", s)
}
