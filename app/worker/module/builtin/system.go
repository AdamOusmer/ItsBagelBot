package builtin

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// websiteURL is what !itsbagelbot points chat at. System command, code-only.
const websiteURL = "https://itsbagelbot.dev"

const (
	bagelMessage = "🥯"
	bagelCount   = 3
)

// SystemModule is the hidden, always-on core module. It is never listed on the
// dashboard and cannot be toggled or reconfigured by a broadcaster. It owns two
// things:
//
//   - the reserved system commands (!ping, !itsbagelbot, ...), which a custom or
//     default command can never shadow (CommandModule skips these names);
//   - the bagel greeting: a special user's first message on a live stream (any
//     message, command or not) gets three bagels, once per stream.
type SystemModule struct {
	special   *module.SpecialSet
	live      module.LiveStore
	greet     module.GreetStore
	startedAt time.Time
	log       *zap.Logger
}

func NewSystemModule(special *module.SpecialSet, live module.LiveStore, greet module.GreetStore, log *zap.Logger) *SystemModule {
	return &SystemModule{special: special, live: live, greet: greet, startedAt: time.Now(), log: log}
}

func (m *SystemModule) Name() string     { return "" } // core: always on, never listed
func (m *SystemModule) Events() []string { return []string{"channel.chat.message"} }

func (m *SystemModule) Handle(ctx context.Context, c *module.Context) ([]*outgress.Message, error) {
	var out []*outgress.Message

	// Bagel: any first message from a special user while the stream is live.
	if greets := m.bagel(ctx, c); len(greets) > 0 {
		out = append(out, greets...)
	}

	// Reserved system command.
	if name, _, ok := parseCommand(c.Env.Text); ok {
		if resp, isSys := m.systemResponse(name); isSys && resp != "" {
			out = append(out, chatReply(c.Env.BroadcasterUserID, resp))
		}
	}

	return out, nil
}

// bagel returns the bagel greeting messages, or nil. Errors are logged and
// swallowed so they never suppress a system command on the same message.
func (m *SystemModule) bagel(ctx context.Context, c *module.Context) []*outgress.Message {
	if !m.special.Has(c.Env.ChatterUserID) {
		return nil
	}

	live, err := m.live.IsLive(ctx, c.BroadcasterID)
	if err != nil {
		m.log.Warn("system: live check failed for bagel", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return nil
	}
	if !live {
		return nil
	}

	first, err := m.greet.FirstGreet(ctx, c.BroadcasterID, c.Env.ChatterUserID)
	if err != nil {
		m.log.Warn("system: greet check failed", zap.Uint64("broadcaster_id", c.BroadcasterID), zap.Error(err))
		return nil
	}
	if !first {
		return nil
	}

	m.log.Debug("bagel greet",
		zap.String("chatter_id", c.Env.ChatterUserID),
		zap.Uint64("broadcaster_id", c.BroadcasterID),
	)
	out := make([]*outgress.Message, 0, bagelCount)
	for i := 0; i < bagelCount; i++ {
		out = append(out, chatReply(c.Env.BroadcasterUserID, bagelMessage))
	}
	return out
}

// systemResponse resolves a reserved system command's reply. The bool is false
// when name is not a system command, which is also how CommandModule knows to
// leave the name alone.
func (m *SystemModule) systemResponse(name string) (string, bool) {
	switch name {
	case "ping":
		return "🏓 up for " + humanizeUptime(time.Since(m.startedAt)), true
	case "itsbagelbot":
		return "ItsBagelBot 🥯 → " + websiteURL, true
	case "source":
		return "Source → https://github.com/AdamOusmer/ItsBagelBot", true
	default:
		return "", false
	}
}

// isSystemCommand reports whether name is a reserved system command. It is the
// gate CommandModule uses so a custom/default command can never shadow one.
func isSystemCommand(name string) bool {
	switch name {
	case "ping", "itsbagelbot", "source":
		return true
	default:
		return false
	}
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
