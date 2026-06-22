package module

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
)

type stubModule struct {
	name   string
	events []string
}

func (s stubModule) Name() string     { return s.name }
func (s stubModule) Events() []string { return s.events }
func (s stubModule) Handle(context.Context, *Context) ([]*outgress.Message, error) {
	return nil, nil
}

func TestRegistryRouting(t *testing.T) {
	core := stubModule{name: "", events: []string{"channel.chat.message"}}
	named := stubModule{name: "bagel", events: []string{"channel.chat.message", "stream.online"}}
	reg := NewRegistry(core, named)

	chat := reg.For("channel.chat.message")
	assert.Len(t, chat, 2)
	assert.Len(t, reg.For("stream.online"), 1)
	assert.Empty(t, reg.For("channel.cheer"))
}

func TestNeedsModuleViews(t *testing.T) {
	core := stubModule{name: "", events: []string{"channel.chat.message"}}
	named := stubModule{name: "bagel", events: []string{"stream.online"}}
	reg := NewRegistry(core, named)

	// Plain chat hits only the core module: no ModuleView fetch needed.
	assert.False(t, reg.NeedsModuleViews("channel.chat.message"))
	// stream.online has a name-gated module: the pipeline must fetch views.
	assert.True(t, reg.NeedsModuleViews("stream.online"))
	assert.False(t, reg.NeedsModuleViews("unmapped"))
}
