package pipeline

import (
	"context"
	"encoding/json"
	"strings"

	"go.uber.org/zap"
)

// The handlers below are the per-type stages registered in NewPipeline. Each
// one owns one EventSub type end to end: resolve what it needs from the
// projection (in-process cache -> Valkey -> projector RPC), decide on an
// action, and return the outgress message to send (or nil to do nothing).
//
// channel.chat.message is implemented as the worked example of the full path;
// the event stages (follow/sub/cheer/raid/stream) are stubs that already pull
// the broadcaster's modules so building the rest of the app is filling in the
// body, not the wiring.

// handleChatMessage resolves a "!command" against the broadcaster's custom
// commands and, when it matches an active command, emits a chat reply.
func (p *Pipeline) handleChatMessage(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	broadcasterID, ok := env.broadcasterID()
	if !ok {
		return nil, nil
	}

	name, _, isCommand := parseCommand(env.Text)
	if !isCommand {
		return nil, nil
	}

	cmd, found, err := p.proj.Command(ctx, broadcasterID, name)
	if err != nil {
		return nil, err
	}
	if !found || !cmd.IsActive || cmd.Response == "" {
		return nil, nil
	}
	if cmd.StreamOnlineOnly {
		user, err := p.proj.User(ctx, broadcasterID)
		if err != nil {
			return nil, err
		}
		if !user.IsLive {
			return nil, nil
		}
	}

	p.log.Debug("chat command matched",
		zap.String("command", name),
		zap.String("regress", regress.String()),
		zap.String("broadcaster_id", env.BroadcasterUserID),
	)

	return chatReply(env.BroadcasterUserID, cmd.Response), nil
}

// handleStream reacts to the live lane (stream.online / stream.offline). It is
// also delivered on the premium/standard event lanes (ingress dual-publishes
// it), so modules keyed off going live can act without watching the live lane.
func (p *Pipeline) handleStream(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	broadcasterID, ok := env.broadcasterID()
	if !ok {
		return nil, nil
	}
	p.log.Debug("stream event",
		zap.String("type", env.Type),
		zap.String("regress", regress.String()),
		zap.Uint64("broadcaster_id", broadcasterID),
	)
	// TODO: live-announcement / greeting modules.
	return nil, nil
}

func (p *Pipeline) handleFollow(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	return p.eventStub(ctx, env, regress, "follow")
}

func (p *Pipeline) handleSubscribe(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	return p.eventStub(ctx, env, regress, "subscribe")
}

func (p *Pipeline) handleCheer(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	return p.eventStub(ctx, env, regress, "cheer")
}

func (p *Pipeline) handleRaid(ctx context.Context, env Envelope, regress Regress) (*OutgressMessage, error) {
	return p.eventStub(ctx, env, regress, "raid")
}

// eventStub is the shared skeleton for the alert-style events. It loads the
// broadcaster's modules so the real handler can branch on which alert modules
// are enabled, then returns nothing until those modules are built.
func (p *Pipeline) eventStub(ctx context.Context, env Envelope, regress Regress, kind string) (*OutgressMessage, error) {
	broadcasterID, ok := env.broadcasterID()
	if !ok {
		return nil, nil
	}

	mods, err := p.proj.Modules(ctx, broadcasterID)
	if err != nil {
		return nil, err
	}

	p.log.Debug("event received",
		zap.String("kind", kind),
		zap.String("regress", regress.String()),
		zap.Uint64("broadcaster_id", broadcasterID),
		zap.Int("modules", len(mods)),
	)
	// TODO: dispatch to the enabled alert modules for this kind.
	return nil, nil
}

// parseCommand extracts the command name and its argument string from chat
// text. "!so @bob hi" -> ("so", "@bob hi", true). Non-commands return false.
func parseCommand(text string) (name, args string, ok bool) {
	trimmed := strings.TrimLeft(text, " ")
	if !strings.HasPrefix(trimmed, "!") {
		return "", "", false
	}
	body := strings.TrimPrefix(trimmed, "!")
	name, args, _ = strings.Cut(body, " ")
	if name == "" {
		return "", "", false
	}
	return strings.ToLower(name), strings.TrimSpace(args), true
}

// chatReply builds the outgress message that sends one chat line. sender_id is
// left for outgress to fill from the bot account it authenticates as.
func chatReply(broadcasterID, message string) *OutgressMessage {
	body, _ := json.Marshal(map[string]string{
		"broadcaster_id": broadcasterID,
		"message":        message,
	})
	return &OutgressMessage{
		Type:          "chat",
		BroadcasterID: broadcasterID,
		Endpoint:      "/helix/chat/messages",
		Method:        "POST",
		Payload:       body,
	}
}
