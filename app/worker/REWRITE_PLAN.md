# Worker + Outgress Rewrite Plan

Status: ready to code. Owner: worker. Scope crosses `app/worker`, `app/outgress`,
`internal/projection`, `internal/domain/outgress`, `app/projector`, `pkg/bus`, `pkg/cache`.

## Goals

1. Recode the worker into real, self-contained modules: **1 module = 1 file**, each
   module **owns its commands**, and each command carries **its own permission** even
   when it shares a module with others.
2. **Baked primitives**: a single immutable module holding all code-defined commands.
   A broadcaster cannot modify, disable, or shadow them.
3. **Minimal processing time, ~0 allocation** on the hot path (plain non-command chat
   must allocate nothing in the pipeline, excluding the JSON decoder's internals).
4. `/announce`, `/announce{blue,green,orange,purple}`, and `/shoutout` become their own
   outgress envelope types that hit the correct Helix endpoints. `/me` falls back to
   plain chat (Twitch's new chat API has no `/me` endpoint).
5. Outgress is faster: no per-message map decode/re-marshal, single route lookup.
6. Heavy, correct caching with reliable invalidation. Continuous code health via
   GRASP/SOLID.

## Decisions (change here if wrong)

- **Override mechanism deleted.** Baked commands are truly immutable. Shipped commands
  that should stay editable become **seeded custom-command rows** plus a one-time seeder.
- **`!bot` = seeded-editable** (a normal, fully editable custom command).
- **`!me` = no baked command.** The slash translator maps a `/me` response to plain-chat
  passthrough; there is no dedicated endpoint.

## Invariants (enforced by tests, not convention)

- Worker is **read-only to Valkey**; only the projector writes it. The read path stays:
  `L1 (theine) -> worker Valkey read -> projector RPC -> projector Valkey read ->
  data-service RPC -> DB`. The projector writes Valkey asynchronously; the worker writes
  only its L1.
- `projection.Reader` is the single seam (DIP). Arch-guard test: `app/worker` must not
  import `ent` / `pkg/db` and must not call `store.Set*`.
- Adding a feature = a new module file + one register line (OCP). No edits to the
  pipeline or registry.
- Hot path (plain non-command chat): 0 heap allocs in the pipeline, proven with
  `-benchmem`.

---

## Caching model (already correct; preserve and tune)

`pkg/cache` is theine (W-TinyLFU LRU, async ring-buffer writes) + singleflight + jittered
TTL. `projection.Client` already implements the 3-tier read with a per-user negative
cache that is only written after the projector RPC misses. Writes are non-blocking
(theine `SetWithTTL` is in-memory; the worker never writes Valkey).

| Layer | Cache | Source order | Invalidation |
|---|---|---|---|
| Baked / slash / typeRoutes | static (L0) | code | n/a |
| Custom command | L1 -> worker Valkey -> projector RPC | read-through + single-flight | commands-service event evicts exact `(user,name)` |
| Negative (not-found) | L1, per-user `command:<id>:<name>` | only after projector RPC confirms absent | command-create event evicts |
| Module views | L1 -> worker Valkey -> projector RPC | name-gated events only | modules-service event |
| Live / greet | L1 -> worker Valkey -> projector | as today | stream events + expiry watcher |

Tuning only (no rewrite): kill `fmt.Sprint` key allocs, raise capacity/TTL (invalidation
is reliable, so TTL is a safety net), delete the now-unused override cache.

---

## Work items

### 1. `internal/domain/outgress/message.go`
- Add `TypeAnnounce = "announce"`, `TypeShoutout = "shoutout"`.
- Add `Color string` and `To string` fields to `Message`.
- Document payloads: announce body `{message, color}` (outgress injects `moderator_id`);
  shoutout carries `To` (login or id; outgress resolves login -> id).

### 2. `pkg/bus/bus.go` — `PublishRaw`
```go
func PublishRaw(ctx context.Context, pub message.Publisher, subject string, body []byte) error
```
Same UUIDv7 + trace-header logic as `PublishJSON`, but accepts pre-marshaled bytes so the
pipeline marshals an `Output` once into a pooled buffer. `PublishJSON` stays for cold
callers.

### 3. `pkg/cache/keys.go` — `PairKey`
```go
func PairKey(prefix string, id uint64, name string) string // strconv.AppendUint, no fmt
```

### 4. `internal/projection/client.go`
- Replace `fmt.Sprint` in `key`/`cmdKey` with `cache.PairKey` / `cache.UserKey`.
- Delete the override path entirely: `Reader.CommandOverride`, the `overrides` cache,
  `CommandOverride()`, `overrideKey`, `overrideEntry`, and the override branch of the
  invalidation listener.
- Constructor takes configurable capacity + TTL; raise hot-cache capacity above 10k.
- `Command()` negative-cache flow is unchanged (already per-user + projector-before-negative).

### 5. `app/projector/projector.go`
- Delete the `command.<name>` routing branch in `HandleModuleChanged`; modules-scope only.

### 6. Worker module core (`app/worker/module/`)

`module.go` — new contract:
```go
type Output struct { Type, BroadcasterID, Text, Color, To string } // pooled
type Emit func(*Output)

type Module interface {
    Name() string             // "" = core, immutable, unlisted
    Events() []string
    Commands() []Command      // baked, immutable; nil if none
    Handle(ctx context.Context, c *Context, emit Emit) error
}

type Command struct {
    Name          string
    Perm          Role
    Cooldown      time.Duration
    LiveOnly      bool
    AllowedUserID string        // "" = role-gated; baked never sets it
    Run           func(ctx context.Context, c *Context, args string, emit Emit) error
}

type PremiumOnly interface{ PremiumOnly() bool }
type Defaulted   interface{ DefaultEnabled() bool }
```
`Context`: add `Reset()` for pooling; keep lazy `Chatter()`; keep `Decode` (shoutout uses it).

`registry.go` — add a baked-command index built once at startup:
```go
func (r *Registry) Command(name string) (Command, bool) // baked only
```
Baked names are reserved; the router checks them first so custom/default can never shadow.

`pool.go` (new) — `sync.Pool` for `lane.Envelope` (retain `Badges` backing array on reset),
`Context`, `Output`, and a scratch `[]byte` buffer. `Get`/`Put`/`Reset` helpers.

`expand.go` (new) — manual template expander into a pooled `[]byte`:
`{user} {sender} {args} {touser} {raider} {raider_login} {viewers}`. Replaces every
`strings.NewReplacer`.

`slash.go` (new) — `Translate(out *Output)` rewrites in place when `out.Text` begins with a
known verb (static table):
- `/announce` -> announce, color primary; `/announce{blue,green,orange,purple}` -> that color; strip verb.
- `/shoutout @x` -> shoutout, `To` = login; strip verb.
- `/me` -> leave as chat (no strip, plain passthrough).

`command_router.go` (new, core) — central dispatch (SRP/DRY). Owns gate enforcement for
both baked and custom commands; no per-message closure allocation.
```go
type CommandRouter struct {
    baked    map[string]Command
    proj     projection.Reader
    live     LiveStore
    cooldown CooldownStore
    log      *zap.Logger
}
func (r *CommandRouter) Name() string     { return "" }
func (r *CommandRouter) Events() []string { return []string{"channel.chat.message"} }
func (r *CommandRouter) Handle(ctx, c, emit) error {
    name, args, ok := parseCommand(c.Env.Text)
    if !ok { return nil }
    if cmd, isBaked := r.baked[name]; isBaked {
        return r.gateAndRun(ctx, c, cmd, args, emit)
    }
    cc, found, err := r.proj.Command(ctx, c.BroadcasterID, name)
    if err != nil || !found || !cc.IsActive { return err }
    return r.runCustom(ctx, c, cc, args, emit) // shared gate(), then expand + slash + emit
}
```
`gate()` is pure (perm / allowed-user / live / cooldown); the cooldown key is built with
`strconv` into a pooled buffer. `runCustom` reuses `gate()` then `expand` -> `slash.Translate`
-> `emit`.

`parseCommand` moves into the `module` package. `permission.go`, `regress.go`, `special.go`,
`greet.go`, `cooldown.go`, `live.go` stay; only `Emit` signature touch-ups.

### 7. Builtin modules (`app/worker/module/builtin/`, 1 file each)

`baked.go` (new; replaces `system.go` + `defaults.go`) — the immutable-primitives module.
- `Commands()`: `!ping` (everyone), `!itsbagelbot`, `!source`, and
  `!announce` / `!announce{blue,green,orange,purple}` (Perm Moderator -> emit announce Output).
- `Handle()`: the bagel greet (special user's first message on a live stream).
- No projection, no cache; uptime / website constants on the struct.

`shoutout.go` — raid module, unchanged in spirit (posts chat text via `Emit`), conformed.

`live.go` — conform to `Emit` (emits nothing).

`command.go` — delete `CommandModule` / `resolve` / `override` / default-table path
(dispatch now lives in `module/command_router.go`). Keep nothing structural.

Delete `system.go`, `defaults.go`. Rewrite `*_test.go` to the new contract.

### 8. Pipeline (`app/worker/pipeline/pipeline.go`)
Rewrite `Process`:
1. sonic decode into a pooled `lane.Envelope` (defer `Put`).
2. self-message + no-module short-circuits (as today).
3. pooled `Context`.
4. module-views read only when `NeedsModuleViews` (unchanged).
5. modules run with an `Emit` closure; per output: `slash.Translate` -> build the outgress
   body in a pooled buffer (manual JSON, no map, no `Marshal`) -> `bus.PublishRaw` -> reset
   Output. Zero alloc when nothing is emitted.
6. ack/nack + per-module error isolation unchanged.

Pipeline must not import `builtin` concretes (DIP guard).

### 9. main (`app/worker/main.go`)
Build the registry, then inject its baked index into the router (two-phase or
`registry.Command`):
```go
baked := builtin.NewBakedModule(special, live, greet, log)
registry := module.NewRegistry(
    baked,
    module.NewCommandRouter(/* baked index */, proj, live, cooldown, log),
    builtin.NewLiveModule(live, greet, log),
    builtin.NewShoutoutModule(log),
)
```
Wire the pools.

### 10. Outgress (`app/outgress/internal/worker/worker.go`)
- `typeRoutes`: add `announce -> {POST, /helix/chat/announcements, AsApp}` and
  `shoutout -> {POST, /helix/chat/shoutouts, AsApp}`.
- Single `typeRoutes` lookup (drop the double `_, mapped` + `r, ok`).
- `processAnnounce`: inject `moderator_id` = bot (query + body per Twitch), pay the general
  bucket, execute.
- `processShoutout`: resolve `To` (login -> id via the twitch client, cached in a small
  in-process cache), set `from_broadcaster_id` = channel, `to_broadcaster_id`,
  `moderator_id` = bot, pay the general bucket.
- `withSenderID` -> manual byte splice (find the last `}`, insert `,"sender_id":"<id>"`);
  reuse as `withField` for `moderator_id`. Removes the per-chat `map[string]json.RawMessage`
  decode/re-marshal.
- Decode the top envelope with sonic.

### 11. Seeder (editable defaults)
One-shot, idempotent: seed `!bot` as a custom-command row through the commands service
(skip when a row exists). Runs from the commands-service bootstrap or an admin RPC.

---

## Tests / benchmarks / guards
- `BenchmarkProcess` (`-benchmem`): plain-chat (assert 0 pipeline allocs), command-hit,
  announce, raid.
- Unit: `expand`; `slash.Translate` (all verbs + colors + `/me` passthrough);
  `CommandRouter` gates (perm / allowed-user / live / cooldown); baked immutability (custom
  cannot shadow a baked name).
- Arch-guard (extend BuildGuard): worker has no `ent`/DB import and no `store.Set*`; pipeline
  has no `builtin` import.
- Outgress: `withSenderID` / `withField` splice correctness; announce/shoutout routing +
  `moderator_id` injection; login -> id resolution cache.
- Rewrite existing `builtin/*_test.go` to the new contract.

## Config / deploy
- New worker env: cache capacity / TTL knobs. New outgress env: shoutout login-cache TTL.
- No new NATS subjects (announce/shoutout ride the existing premium/standard lanes; outgress
  routes by type).
- Flux: image bumps only. No DB migration except the optional `!bot` seed.

---

## GRASP / SOLID mapping
- SRP: 1 module = 1 file; pipeline orchestrates only; outgress transports only; gate logic
  lives once in `CommandRouter`.
- OCP: new feature/verb/endpoint = one table or register entry; no pipeline edits.
- LSP: every module satisfies `Module`; `PremiumOnly` / `Defaulted` via interface assertion.
- ISP: small sink + store interfaces (`Emit`, `LiveStore`, `GreetStore`, `CooldownStore`,
  `projection.Reader`).
- DIP: pipeline depends on `Module` / `Registry` + store interfaces; `main` is the composition root.
- Information Expert: a command owns its perm/cooldown/run; a module owns its commands.
- Controller: `pipeline.Process` is the single use-case controller.
- Polymorphism over type-switch: `Command.Run` / `Module.Handle` replace the reserved-name
  switch; `typeRoutes` replaces the outgress type-switch.
- Pure Fabrication: Registry, Pool, Expander, SlashTranslator, projection cache.
- Protected Variations: baked table, slash table, typeRoutes, cache behind stable seams.

## Build order (caveman sonnet subagents)
1. Wave 1 (parallel): item 1, 2, 3, 10.
2. Wave 2: item 6 (module core) — gates Waves 3-4.
3. Wave 3 (parallel): item 7, 4, 5.
4. Wave 4: item 8 + 9 + 11.
5. Wave 5: tests/benches/arch-guard, then `cavecrew-reviewer` over the diff + `/code-review`
   before any commit.

Each subagent is scoped to its files and returns a caveman diff receipt. Every wave is gated
on `go build ./... && go vet ./... && go test ./...`.
