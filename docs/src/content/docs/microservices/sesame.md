---
title: Sesame
description: The core engine and command processor of ItsBagelBot.
---

Sesame is the core engine of ItsBagelBot. It consumes ingress events, evaluates gates, dispatches commands, runs module event handlers, and routes outputs to the outgress service.

## Architecture

Sesame operates as a highly concurrent NATS JetStream consumer. It pulls from the `twitch.ingress.event.*` subjects across two lanes (Premium and Standard) and drains them into a shared pool with reserved capacity for Premium events.

### The Pipeline

Every message flows through `engine.Pipeline`, an allocation-free (for non-emissions) decoding and dispatch stage:
1. **Decode**: The JSON payload is unmarshaled into a pooled `lane.Envelope`.
2. **Deduplication**: `ValkeyDedup` ensures that an event is only processed once, claiming a short-lived key in Valkey based on the event ID.
3. **Module Views**: If the event requires configurable behavior, the `Projector` is queried for the broadcaster's `ModuleView` set.
4. **Command Dispatch**: If the event is a chat message (`channel.chat.message`), the pipeline checks the command registry. Baked commands are evaluated for their permissions, cooldowns, and live-only gates.
5. **Event Handlers**: Non-command event handlers registered by modules are executed in registration order.
6. **Emission**: Module handlers do not publish directly to NATS. They yield an `Output` struct to an `Emit` callback, which builds the `outgress.Message` wire contract and publishes it to either `twitch.outgress.premium` or `twitch.outgress.standard`.

### Module Authoring

Sesame's feature set is authored in the `module` package. To ensure high testability and to keep the authoring surface completely free of runtime wiring (like Valkey, Projector, or NATS), features are declared using a **fluent builder pattern**.

A module is instantiated, its commands and event handlers are chained, and it returns an immutable `Module` that the `engine.Registry` consumes at startup.

#### The Fluent Builder

Here is an example of authoring a module using the builder:

```go
func MyModule() module.Module {
    // 1. Initialize with name and kind (Core, Default, Opt-In)
	m := module.NewModule("my_feature", module.KindDefault)

    // 2. Register non-command EventSub handlers
	m.On("channel.chat.message", handleChat)
    m.On("stream.online", handleStreamOnline)

    // 3. Declare commands with chained gates
	m.Command("ping").Everyone().Run(pingRun)
	m.Command("announce").Mod().Cooldown(10 * time.Second).Run(announceRun)
    m.Command("shoutout").Aliases("so").Mod().LiveOnly().Run(soRun)

    // 4. Validate and build the immutable artifact
	return m.Build()
}
```

#### Module Kinds
Modules are declared as one of three kinds, dictating their enablement logic:
- **Core**: Always enabled, never toggled, skips projection fetches.
- **Default**: Enabled by default, can be disabled via the dashboard.
- **Opt-In**: Disabled by default, must be explicitly enabled via the dashboard.

#### Command Gates
The `.Command("name")` method returns a `CmdBuilder` allowing you to seamlessly chain execution gates before finalizing with `.Run()`.
- **Permissions**: `.Everyone()`, `.Sub()`, `.VIP()`, `.Mod()`, `.Broadcaster()`
- **State**: `.LiveOnly()`, `.Cooldown(time.Duration)`
- **Routing**: `.Aliases("trigger")`, `.NumericSuffix()` (absorbs trailing digits inline, e.g. `!clip30` resolves to `clip`).

The `engine.Registry` indexes these built modules at startup, constructing a flat, case-insensitive command index and a routing table for event types.

### Variables & Templating

Sesame provides a fast, allocation-light string templating system used for dynamic command replies. The `module.Expand` (and `module.ExpandString`) functions parse strings for `{key}` tokens and resolve them using a provided callback.

The system supports passing through literal `{key}` tokens if the callback does not recognize them, instead of silently dropping them. 

Additionally, the `module.ParseDynamic` helper provides built-in support for generic dynamic variables:
- `{random}`: Generates a random number between 1 and 100.
- `{random:min-max}`: Generates a random number between `min` and `max`.
- `{choice:a,b,c}`: Picks a random string from a comma-separated list of choices.

### State & Caching

Sesame maintains high throughput by avoiding database reads on the hot path:
- **Projection Cache**: `projection.Reader` provides an in-memory cache of broadcaster settings (modules, users, custom commands), falling back to NATS RPC (`bagel.rpc.internal.projection.*`) and listening for `bagel.cache.invalidate.*` broadcasts.
- **Live Store**: `ValkeyLiveStore` checks if a broadcaster is currently live. It caches locally, falls back to Valkey, and can trigger a system outgress lane check to Twitch if the key is cold.
- **Cooldown & Dedup**: Backed directly by Valkey.
