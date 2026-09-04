---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Sesame Module Builder
description: The fluent authoring API for sesame modules and commands, with every builder method and the gate order the engine applies.
---

A sesame feature is one function returning one `module.Module` value. The builder
in `app/twitch/sesame/module/builder.go` assembles that value; the engine in
`app/twitch/sesame/engine/` indexes and runs it.

The builder package carries **no runtime wiring** (no NATS, Valkey, projection or
pipeline). A module receives `engine.Deps` and its handlers capture what they need
by closure, which is what keeps the authoring surface small and unit-testable
without a cluster.

## Minimum module

```go
func Ping(d engine.Deps) module.Module {
    m := module.NewModule("", module.KindCore)
    m.Command("ping").Everyone().Run(
        func(ctx context.Context, c *module.Context, args string, emit module.Emit) error {
            emit(&module.Output{
                Type:          outgress.TypeChat,
                BroadcasterID: c.Env.BroadcasterUserID,
                Text:          "pong",
            })
            return nil
        })
    return m.Build()
}
```

Then add one line to `app/twitch/sesame/modules/all.go`. That is the whole registration
step.

## Module-level API

| Call | Purpose |
|---|---|
| `module.NewModule(name, kind)` | Start a module. Returns `*Builder`. |
| `b.Command(name)` | Begin a chat command. Returns `*CmdBuilder` to chain gates. Name is lowercased. |
| `b.On(eventType, fn)` | Register a non-command handler for one EventSub type. Registering the same type twice keeps the last handler. |
| `b.Build()` | Validate and return the immutable `Module`. **Panics** on a programmer error. |
| `b.Validate()` | Same checks without panicking. For tests. |

A `Builder` is single-use and not safe for concurrent use.

## Kinds

| Kind | Name | Behaviour |
|---|---|---|
| `KindCore` | optional | Always on. Never listed to broadcasters, never toggled, no config. The engine skips the `ModuleView` projection fetch entirely for it. |
| `KindDefault` | required | Named, ships **enabled**. Runs unless the broadcaster disables it. |
| `KindOptIn` | required | Named, ships **disabled**. Runs only when the broadcaster enables it. |

`Build` enforces the pairing: a named kind with an empty name fails.

Core skipping the projection fetch is a hot-path property, not a detail. A named
module costs a `ModuleView` lookup per message to decide whether it runs. Core
costs nothing. A core module that still wants a per-broadcaster toggle does its
own lazy check inside `Run` (see `moduleEnabled` in `modules/followage.go`).

## Command gates

Every method below returns the same `*CmdBuilder`, so they chain. `Run` is
terminal and returns nothing.

### Permission

Each sets the **minimum** role, so higher roles also pass.

| Method | Minimum role |
|---|---|
| `.Everyone()` | anyone in chat (the default) |
| `.Sub()` | subscriber |
| `.VIP()` | VIP |
| `.Mod()` | moderator |
| `.Broadcaster()` | channel owner only |

Ladder order: `RoleEveryone` → `RoleSubscriber` → `RoleVIP` → `RoleModerator` →
`RoleLeadModerator` → `RoleBroadcaster`.

**There is no `.LeadMod()` builder method**, even though `RoleLeadModerator` sits
in the ladder. That entry exists for *inclusion*, not gating: Twitch ships the
`lead_moderator` badge **instead of** `moderator`, so without a rank above
moderator every lead mod would fail a `.Mod()` gate for want of a recognised
badge. `.Mod()` therefore already admits lead moderators and above.

The tier is otherwise first-class: `permRoles` maps `"lead_mod"`,
`internal/domain/validate` accepts it, the `commands` ent schema stores it, and
the dashboard offers "Lead moderators & up". So a broadcaster-authored **custom**
command can be lead-mod-only (the engine gates baked and custom commands through
the same `gate()`), while a **baked** module currently cannot express "lead mods
and up, excluding plain mods". Adding `.LeadMod()` would be a one-line
`CmdBuilder` setter if a built-in ever needs it.

| Method | Purpose |
|---|---|
| `.AllowUser(id)` | Restrict to exactly one chatter id. **Overrides the role gate entirely.** |

### Conditions

| Method | Purpose |
|---|---|
| `.Cooldown(d)` | Shared per-command window. Zero (default) means none. |
| `.LiveOnly()` | Only runs while the broadcaster is live. |

### Matching

| Method | Purpose |
|---|---|
| `.Aliases(a, b, ...)` | Extra triggers resolving to this command. Lowercased. Duplicates rejected by `Validate`. |
| `.NumericSuffix()` | Trigger absorbs trailing digits typed inline: `!clip30` resolves to `clip` with `c.Num == "30"`. Digits are stripped and are **not** the argument string. |

### Terminal

| Method | Purpose |
|---|---|
| `.Run(fn)` | Sets the handler and finishes the command. Returns nothing so a declaration cannot continue past it. |

## Gate order

The engine applies gates centrally in `engine/dispatch.go`:

```go
func (p *Pipeline) gate(ctx, c, r gateRule) (bool, error) {
    if !permits(c, r.allowedUserID, r.perm) { return false, nil }          // 1
    if ok, err := p.liveOK(ctx, c, r.liveOnly); !ok { return false, err }  // 2
    return p.cooldownOK(ctx, c.BroadcasterID, r.name, r.cooldown)          // 3
}
```

1. permission
2. live-only
3. cooldown

**Cooldown is last on purpose.** `cooldownOK` *claims* the window when it passes.
If it ran first, a chatter who fails the permission check would still burn
everyone else's cooldown.

Baked commands and broadcaster-authored custom commands build the same
`gateRule`, so permission semantics cannot drift between built-ins and user
commands.

## What `Run` receives

```go
type RunFunc func(ctx context.Context, c *Context, args string, emit Emit) error
```

- `args` is the trimmed string **after** the command name.
- `c.Env` is the chat envelope: `BroadcasterUserID`, `ChatterUserID`,
  `ChatterUserLogin`, `ChatterName()`, badges.
- `c.Chatter()` resolves the chatter's role, parsed once and cached.
- `c.Locale` is the broadcaster's language. Use `i18n.T(c.Locale, "key")`.
- `c.Config` is this module's raw config blob; `c.Decode(&out)` unmarshals it.
  Empty for core modules, and a missing config is not an error.
- `c.Num` is the inline numeric suffix, set only for a `NumericSuffix` command.

### Pooling rules

Both of these will corrupt other messages if ignored.

- **Do not retain `c` past the call.** `Context` is pooled and `Reset()` between
  messages.
- **Do not retain the `Output` passed to `emit`.** The engine may recycle it as
  soon as `Emit` returns.

Copy anything that must outlive the call.

## Emitting

```go
emit(&module.Output{
    Type:          outgress.TypeChat,
    BroadcasterID: c.Env.BroadcasterUserID,
    Text:          "hello",
})
```

`Output.Type` selects the action (`outgress.TypeChat`, `TypeAnnounce`, clip,
shoutout, moderation, redemption resolution). Type-specific fields are documented
on the struct in `module/module.go`; fields belonging to other types are ignored.
`BatchID`/`Items` carry a multi-message response as one outgress queue job,
executed in slice order.

## Validation

`Build` panics rather than returning an error, because these are startup
misconfigurations rather than runtime data, and failing loud at boot beats
silently misbehaving in production. It rejects:

- a named kind with an empty name
- an unknown kind
- an empty command name or empty alias
- a duplicate trigger **within the same module**
- a command with no `Run` (reported as "chain .Run to finish it")

A command you forget to finish still exists as an incomplete entry, because
`Command()` appends it before any chaining happens, which is exactly how
`Validate` catches it.

## Cross-module collisions

Duplicates *within* a module panic at build. Duplicates *across* modules are
**first-wins** with a warning log (`engine: duplicate command trigger ignored`).

Ordering in `all.go` is therefore load-bearing: core modules are listed first so
their reserved triggers win over any named module declaring a clash.

## Checklist for a new module

1. Create `app/twitch/sesame/modules/<name>.go`.
2. `func <Name>(d engine.Deps) module.Module`.
3. `module.NewModule(name, kind)`.
4. Declare commands with `.Command(...).<gates>.Run(fn)` and events with
   `.On(type, fn)`.
5. `return m.Build()`.
6. Add one line to `all.go`.
7. Localise replies via `i18n.T(c.Locale, key)`.
8. Guard optional services (`d.X != nil`) and decide the failure policy
   deliberately; existing modules fail **open** so a transient projection blip
   does not swallow a command.
9. Add `<name>_test.go`. `Validate()` runs without a cluster.
