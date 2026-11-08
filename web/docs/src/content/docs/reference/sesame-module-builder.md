---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Sesame module builder
description: "Define and register Sesame modules and commands."
docType: how-to
---

## Outcome and prerequisites

The builder in `app/twitch/sesame/module/builder.go` creates a `module.Module`; the engine indexes and runs the result. Module constructors are registered in `app/twitch/sesame/modules/all.go`. Existing modules in that directory are the best examples of dependency wiring and handlers.

## Declare a module

```go
m := module.NewModule("example", module.KindOptIn)
m.Command("hello").Everyone().Run(handler)
return m.Build()
```

Here `handler` must satisfy `module.RunFunc`. `Command` lowercases the name; `Run` finishes its definition. A module can also register an event handler with `On`. `KindCore` is for core behaviour, `KindDefault` for named modules enabled by default, and `KindOptIn` for named modules enabled explicitly. Named kinds require a name.

Command builders include role gates (`Everyone`, `Sub`, `VIP`, `Mod`, `LeadMod`, `Broadcaster`), `Cooldown`, `LiveOnly`, `Aliases`, `When` and `AllowUser`. The builder records these declarations; the engine applies them. Read the engine and existing module tests before relying on a specific order or adding a gate.

`Validate` returns an error for invalid declarations, including duplicate command triggers or a missing handler. `Build` calls it and panics on an error, making mistakes fail at startup. Trial modules must be core and cannot be beta; core modules cannot be beta. See the builder and its tests for the complete rules, then run the focused module and engine tests when changing them.

## Add a module

1. Implement a constructor alongside the existing [modules](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/modules/). Choose a kind and capture only the dependencies its handlers need from `engine.Deps`.
2. Declare commands and event handlers with the [builder](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/sesame/module/builder.go). Give each command a `Run` handler and unique names and aliases within the module. Use `Validate` in focused tests for invalid declarations.
3. Register the constructor in [`modules.All`](https://github.com/AdamOusmer/ItsBagelBot/blob/main/app/twitch/sesame/modules/all.go). For a configurable module, trace its configuration and enabled state through the modules service and projection; registration alone does not create that data.
4. Test the handler's output and error cases, the relevant engine gates and any changes to public variables. Run `go test ./app/twitch/sesame/module/... ./app/twitch/sesame/engine/...` plus the changed module's tests.

`Build` copies the command slice and event map into the returned value; the builder itself is not runtime wiring. The engine's actual gate order belongs to the implementation and tests under [engine](https://github.com/AdamOusmer/ItsBagelBot/tree/main/app/twitch/sesame/engine/), not the order in which this page lists builder methods.

## Verify and troubleshoot

A successful focused test run exits with status zero. If startup panics at `Build`, call `Validate` in a test and inspect the error: a command may have no handler, an alias may duplicate a trigger, or the module kind and flags may be incompatible. If a command is registered but does not run, inspect its engine gates and the channel's module state before changing the handler.

See [module variables](/reference/module-variables/) when exposing public reply values and [testing](/guides/testing/) for broader checks.
