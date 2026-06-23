package builtin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"

	"go.uber.org/zap"
)

// defaultShoutoutTemplate is used when the broadcaster has not set one. Tokens:
// {raider} display name, {raider_login} login, {viewers} raid party size.
const defaultShoutoutTemplate = "🥯 Huge shoutout to {raider} who raided with {viewers}! Go show some love → twitch.tv/{raider_login}"

// ShoutoutModule posts a shoutout when another channel raids in. It is a named,
// opt-in module: a broadcaster enables it on the dashboard and customizes the
// message template via config. Off by default.
type ShoutoutModule struct {
	log *zap.Logger
}

func NewShoutoutModule(log *zap.Logger) *ShoutoutModule { return &ShoutoutModule{log: log} }

func (m *ShoutoutModule) Name() string               { return "shoutout" }
func (m *ShoutoutModule) Events() []string           { return []string{"channel.raid"} }
func (m *ShoutoutModule) DefaultEnabled() bool       { return false } // opt-in
func (m *ShoutoutModule) Commands() []module.Command { return nil }

type shoutoutConfig struct {
	Message string `json:"message"`
}

// raidEvent is the subset of the channel.raid EventSub payload we use.
type raidEvent struct {
	FromBroadcasterUserLogin string `json:"from_broadcaster_user_login"`
	FromBroadcasterUserName  string `json:"from_broadcaster_user_name"`
	ToBroadcasterUserID      string `json:"to_broadcaster_user_id"`
	Viewers                  int    `json:"viewers"`
}

// Handle decodes the raid event, builds the shoutout line, and emits one chat
// message to the receiving channel. Raids are rare, so the per-call Replacer
// allocation is fine.
func (m *ShoutoutModule) Handle(_ context.Context, c *module.Context, emit module.Emit) error {
	if len(c.Env.Event) == 0 {
		return nil
	}
	var ev raidEvent
	if err := json.Unmarshal(c.Env.Event, &ev); err != nil {
		return err
	}
	if ev.FromBroadcasterUserLogin == "" {
		return nil
	}

	tmpl := defaultShoutoutTemplate
	var cfg shoutoutConfig
	if err := c.Decode(&cfg); err == nil && cfg.Message != "" {
		tmpl = cfg.Message
	}

	raider := ev.FromBroadcasterUserName
	if raider == "" {
		raider = ev.FromBroadcasterUserLogin
	}
	msg := strings.NewReplacer(
		"{raider}", raider,
		"{raider_login}", ev.FromBroadcasterUserLogin,
		"{viewers}", strconv.Itoa(ev.Viewers),
	).Replace(tmpl)

	// The raid event names the receiving channel as to_broadcaster_user_id.
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: ev.ToBroadcasterUserID,
		Text:          msg,
	})
	return nil
}
