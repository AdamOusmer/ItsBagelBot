package module

import (
	"context"
	"testing"
	"time"
)

// noop is a valid RunFunc/EventHandler for tests that only care about assembly.
func noopRun(context.Context, *Context, string, Emit) error { return nil }
func noopEvt(context.Context, *Context, Emit) error         { return nil }

func TestBuildAssemblesCommandsAndEvents(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("ping").Everyone().Run(noopRun)
	m.Command("announce").Mod().Run(noopRun)
	m.On("channel.chat.message", noopEvt)
	mod := m.Build()

	if mod.Name != "" || mod.Kind != KindCore {
		t.Fatalf("got name=%q kind=%v, want core with empty name", mod.Name, mod.Kind)
	}
	if len(mod.Commands) != 2 {
		t.Fatalf("got %d commands, want 2", len(mod.Commands))
	}
	if mod.Commands[0].Name != "ping" || mod.Commands[0].Perm != RoleEveryone {
		t.Errorf("ping: got name=%q perm=%v", mod.Commands[0].Name, mod.Commands[0].Perm)
	}
	if mod.Commands[1].Name != "announce" || mod.Commands[1].Perm != RoleModerator {
		t.Errorf("announce: got name=%q perm=%v", mod.Commands[1].Name, mod.Commands[1].Perm)
	}
	if _, ok := mod.Events["channel.chat.message"]; !ok {
		t.Errorf("event handler not registered, events=%v", mod.Events)
	}
}

func TestPermSettersMapToRoles(t *testing.T) {
	cases := []struct {
		name string
		set  func(*CmdBuilder) *CmdBuilder
		want Role
	}{
		{"everyone", (*CmdBuilder).Everyone, RoleEveryone},
		{"sub", (*CmdBuilder).Sub, RoleSubscriber},
		{"vip", (*CmdBuilder).VIP, RoleVIP},
		{"mod", (*CmdBuilder).Mod, RoleModerator},
		{"broadcaster", (*CmdBuilder).Broadcaster, RoleBroadcaster},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModule("", KindCore)
			tc.set(m.Command(tc.name)).Run(noopRun)
			mod := m.Build()
			if got := mod.Commands[0].Perm; got != tc.want {
				t.Fatalf("got perm=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestCommandOptionsLand(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("so").
		Mod().
		Cooldown(30 * time.Second).
		LiveOnly().
		AllowUser("12345").
		Aliases("shoutout", "SO2").
		Run(noopRun)
	cmd := m.Build().Commands[0]

	if cmd.Cooldown != 30*time.Second {
		t.Errorf("cooldown: got %v", cmd.Cooldown)
	}
	if !cmd.LiveOnly {
		t.Errorf("LiveOnly not set")
	}
	if cmd.AllowedUserID != "12345" {
		t.Errorf("AllowedUserID: got %q", cmd.AllowedUserID)
	}
	if len(cmd.Aliases) != 2 || cmd.Aliases[0] != "shoutout" || cmd.Aliases[1] != "so2" {
		t.Errorf("aliases not lowercased/stored: got %v", cmd.Aliases)
	}
}

func TestTriggersLowercased(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("PiNg").Run(noopRun)
	if got := m.Build().Commands[0].Name; got != "ping" {
		t.Fatalf("command name not lowercased: got %q", got)
	}
}

func TestValidateKindNamePairing(t *testing.T) {
	cases := []struct {
		name    string
		build   func() *Builder
		wantErr bool
	}{
		{"core empty ok", func() *Builder { return NewModule("", KindCore) }, false},
		{"core named bad", func() *Builder { return NewModule("x", KindCore) }, true},
		{"default named ok", func() *Builder {
			b := NewModule("greeter", KindDefault)
			b.On("channel.chat.message", noopEvt)
			return b
		}, false},
		{"default empty bad", func() *Builder { return NewModule("", KindDefault) }, true},
		{"optin empty bad", func() *Builder { return NewModule("", KindOptIn) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.build().Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateCommandWithoutRun(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("ping") // no .Run
	if err := m.Validate(); err == nil {
		t.Fatal("want error for command without Run, got nil")
	}
}

func TestValidateDuplicateName(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("ping").Run(noopRun)
	m.Command("ping").Run(noopRun)
	if err := m.Validate(); err == nil {
		t.Fatal("want error for duplicate command name, got nil")
	}
}

func TestValidateAliasCollidesWithName(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("ping").Run(noopRun)
	m.Command("pong").Aliases("ping").Run(noopRun)
	if err := m.Validate(); err == nil {
		t.Fatal("want error for alias colliding with a command name, got nil")
	}
}

func TestValidateAliasCollidesWithAlias(t *testing.T) {
	m := NewModule("", KindCore)
	m.Command("a").Aliases("x").Run(noopRun)
	m.Command("b").Aliases("x").Run(noopRun)
	if err := m.Validate(); err == nil {
		t.Fatal("want error for alias colliding with another alias, got nil")
	}
}

func TestBuildPanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Build did not panic on an invalid module")
		}
	}()
	NewModule("x", KindCore).Build() // core module with a name: must panic
}

func TestDuplicateEventKeepsLast(t *testing.T) {
	var last int
	first := func(context.Context, *Context, Emit) error { last = 1; return nil }
	second := func(context.Context, *Context, Emit) error { last = 2; return nil }

	m := NewModule("", KindCore)
	m.On("channel.chat.message", first)
	m.On("channel.chat.message", second)
	mod := m.Build()

	_ = mod.Events["channel.chat.message"](context.Background(), &Context{}, func(*Output) {})
	if last != 2 {
		t.Fatalf("On did not keep the last handler: last=%d", last)
	}
}
