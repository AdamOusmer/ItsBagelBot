// Package compare wires the legacy worker pipeline and the new sesame engine
// against the SAME in-memory fakes so they can be driven with identical inputs
// and their published outgress messages compared. It backs both the parity test
// (compare_test.go) and the runnable stage (app/sesame/cmd/stage), which feeds
// scripted chat/raid envelopes on the two lanes and prints worker vs sesame side
// by side. Nothing here touches real NATS or Valkey: the Reader is the "fake
// Valkey" (an in-memory custom-command set + module toggles), and the stores are
// trivial fakes.
package compare

import (
	"context"
	"strings"
	"time"

	sengine "ItsBagelBot/app/sesame/engine"
	smods "ItsBagelBot/app/sesame/modules"
	wmod "ItsBagelBot/app/worker/module"
	wbuiltin "ItsBagelBot/app/worker/module/builtin"
	wpipe "ItsBagelBot/app/worker/pipeline"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

// Outgress lane subjects the pipelines publish on (fixed for the harness).
const (
	PremiumSubj  = "outgress.premium"
	StandardSubj = "outgress.standard"
)

// Captured is one published outgress message plus the subject it rode.
type Captured struct {
	Subject string
	Msg     outgress.Message
}

// Pub is a message.Publisher that records what was published.
type Pub struct {
	Got  []Captured
	Fail error
}

func (p *Pub) Publish(subject string, msgs ...*message.Message) error {
	if p.Fail != nil {
		return p.Fail
	}
	for _, m := range msgs {
		var om outgress.Message
		_ = sonic.Unmarshal(m.Payload, &om)
		p.Got = append(p.Got, Captured{Subject: subject, Msg: om})
	}
	return nil
}
func (p *Pub) Close() error { return nil }

// Live is a fake live store; it reports IsLiveV and no-ops its writes. It
// satisfies both wmod.LiveStore and sengine.LiveStore (identical method sets).
type Live struct{ IsLiveV bool }

func (f Live) IsLive(context.Context, uint64) (bool, error) { return f.IsLiveV, nil }
func (f Live) SetLive(context.Context, uint64) error        { return nil }
func (f Live) ClearLive(context.Context, uint64) error      { return nil }

// Greet is a fake greet store; FirstGreet returns First.
type Greet struct{ First bool }

func (f Greet) FirstGreet(context.Context, uint64, string) (bool, error) { return f.First, nil }
func (f Greet) ResetGreets(context.Context, uint64) error                { return nil }

// Cooldown never gates. Satisfies both CooldownStore interfaces.
type Cooldown struct{}

func (Cooldown) Allow(context.Context, string, time.Duration) (bool, error) { return true, nil }

// Reader is the fake Valkey: an in-memory user, module-toggle set, and custom
// command set keyed by lowercased name (aliases resolved by scan).
type Reader struct {
	Usr  projection.User
	Mods []projection.ModuleView
	Cmds map[string]projection.Command
}

func (r Reader) User(context.Context, uint64) (projection.User, error) { return r.Usr, nil }
func (r Reader) Modules(context.Context, uint64) ([]projection.ModuleView, error) {
	return r.Mods, nil
}
func (r Reader) Command(_ context.Context, _ uint64, name string) (projection.Command, bool, error) {
	n := strings.ToLower(name)
	if c, ok := r.Cmds[n]; ok {
		return c, true, nil
	}
	for _, c := range r.Cmds {
		for _, a := range c.Aliases {
			if strings.ToLower(a) == n {
				return c, true, nil
			}
		}
	}
	return projection.Command{}, false, nil
}

// Processor is the shared surface: both pipelines take one decoded message.
type Processor interface {
	Process(*message.Message) error
}

// NewWorker builds the legacy worker pipeline with the given fakes.
func NewWorker(pub message.Publisher, reader projection.Reader, special string, live wmod.LiveStore, greet wmod.GreetStore) Processor {
	router := wmod.NewCommandRouter(reader, live, Cooldown{}, nil, zap.NewNop())
	reg := wmod.NewRegistry(zap.NewNop(),
		wbuiltin.NewBakedModule(wmod.NewSpecialSet(special), live, greet, zap.NewNop()),
		router,
		wbuiltin.NewLiveModule(live, greet, zap.NewNop()),
		wbuiltin.NewShoutoutModule(zap.NewNop()),
	)
	router.Bind(reg)
	return wpipe.NewPipeline(zap.NewNop(), pub, reader, reg, "", PremiumSubj, StandardSubj)
}

// NewSesame builds the sesame engine pipeline with the given fakes.
func NewSesame(pub message.Publisher, reader projection.Reader, special string, live sengine.LiveStore, greet sengine.GreetStore) Processor {
	d := sengine.Deps{
		Proj:     reader,
		Live:     live,
		Greet:    greet,
		Cooldown: Cooldown{},
		Special:  sengine.NewSpecialSet(special),
		Pub:      pub,
		Log:      zap.NewNop(),
	}
	reg := sengine.NewRegistry(zap.NewNop(), smods.All(d)...)
	return sengine.NewPipeline(d, reg, sengine.Config{OutgressPremium: PremiumSubj, OutgressStandard: StandardSubj})
}

// ChatMsg builds a channel.chat.message envelope on the given lane, with any
// badge set_ids attached to the chatter (for perm gating).
func ChatMsg(text, chatterID, lane string, badgeSetIDs ...string) *message.Message {
	env := map[string]any{
		"type":                "channel.chat.message",
		"lane":                lane,
		"broadcaster_user_id": "2",
		"chatter_user_id":     chatterID,
		"chatter_user_login":  "alice",
		"text":                text,
	}
	if len(badgeSetIDs) > 0 {
		badges := make([]map[string]string, 0, len(badgeSetIDs))
		for _, id := range badgeSetIDs {
			badges = append(badges, map[string]string{"set_id": id})
		}
		env["badges"] = badges
	}
	body, _ := sonic.Marshal(env)
	return message.NewMessage("uuid", body)
}

// RaidMsg builds a channel.raid envelope on the given lane (receiving channel 2).
func RaidMsg(lane string) *message.Message {
	body, _ := sonic.Marshal(map[string]any{
		"type": "channel.raid",
		"lane": lane,
		"event": map[string]any{
			"from_broadcaster_user_login": "coolstreamer",
			"from_broadcaster_user_name":  "CoolStreamer",
			"to_broadcaster_user_id":      "2",
			"viewers":                     42,
		},
	})
	return message.NewMessage("uuid", body)
}

// ProcessBoth runs the same message through fresh worker and sesame pipelines
// (fresh fakes each, so greet/live state does not leak between them) and returns
// what each published.
func ProcessBoth(msg *message.Message, reader projection.Reader, special string, live, greet bool) (worker, sesame []Captured) {
	wp := &Pub{}
	NewWorker(wp, reader, special, Live{live}, Greet{greet}).Process(msg)
	sp := &Pub{}
	NewSesame(sp, reader, special, Live{live}, Greet{greet}).Process(msg)
	return wp.Got, sp.Got
}
