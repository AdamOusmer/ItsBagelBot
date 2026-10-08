# Discord ingress context

Agent guide for the Discord gateway receiver. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for ownership across the Discord split.

## Purpose and boundary

Ingress owns the fleet bot's gateway Identify session, heartbeat, Resume/reconnect,
presence and gateway health. It wraps selected dispatches as events for Discord
engine. It does not decide community behavior or execute general Discord REST.
The one REST exception is deferring an interaction before Discord's three-second
acknowledgement deadline; engine/outgress finish it later through a webhook.
Two ingress replicas run for availability, but only the Valkey lease holder may
hold a gateway socket per bot token. The standby holds no socket. Engine/outgress
can scale without additional gateway Identify sessions.

## Local nomenclature

- **Gateway:** Discord WebSocket session, not the Gossip integration service.
- **Identify:** opens a new authenticated session and consumes session-start budget.
- **Resume:** reconnects an existing session using its session ID/last sequence.
- **Dispatch:** gateway event carrying Discord's verbatim type and raw JSON body.
- **Event:** internal routing envelope published by relay, with receive timestamp.
- **Interaction defer:** type-5 acknowledgement creating pending followup work;
  it does not mean the command or button action succeeded.
- **Presence:** gateway activity/status derived from narrow users counts RPC.
- **Connect budget:** shared restart-resistant cap/backoff for opening sockets.
- **Bot status:** Valkey projection of the lease holder's actual session condition.
- **Lease:** Valkey `OwnerLock` (TTL 30s, renewed every 5s, standby polls every 2s);
  owner is pod name plus boot ID.
- **Checkpoint:** `{SessionID, ResumeURL, Seq}` in Valkey, saved on READY/RESUMED
  and every 5s or 100 seqs; the successor Resumes from it.
- **Pod/boot ID/connect sequence:** separate process/attempt identity in logs.
- **Guild:** Discord server, bound to a Twitch broadcaster by the data service;
  ingress does not resolve that relationship.

## Code navigation

| Change | Read first |
| --- | --- |
| Boot and health | [main.go](main.go): `main`, `healthSet`; [internal/config/config.go](internal/config/config.go) |
| Gateway lifecycle | [internal/gateway/session.go](internal/gateway/session.go): `Session.Run`, `oneSocket`, `onHello`, `onDispatch`, `heartbeat` |
| Socket serialization and dial | [socket.go](internal/gateway/socket.go), [dial.go](internal/gateway/dial.go), [payload.go](internal/gateway/payload.go) |
| Resume, close codes and retry | [resume.go](internal/gateway/resume.go), [reconnect.go](internal/gateway/reconnect.go) |
| Persisted connection budget | [budget.go](internal/gateway/budget.go), [internal/botstatus/connectlog.go](internal/botstatus/connectlog.go) |
| Lease, standby/leader loop and drain | [internal/gateway/lease.go](internal/gateway/lease.go): `runLeased`, `standby`, `takeLease`, `lead`, `renewWhileHeld`, `drain` |
| Resume checkpoint store | [checkpoint.go](internal/gateway/checkpoint.go) |
| Wrap/routing/publish | [internal/relay/relay.go](internal/relay/relay.go): `Dispatch`, `publish`, `routeFields` |
| Immediate interaction ACK | [internal/relay/ack.go](internal/relay/ack.go): `deferInteraction` |
| Gateway status projection | [internal/botstatus/botstatus.go](internal/botstatus/botstatus.go) |
| Presence reads/format | [internal/presence/source.go](internal/presence/source.go), [client.go](internal/presence/client.go), [format.go](internal/presence/format.go) |
| Shared wire contracts | [Event](../../../internal/domain/discord/event.go), [stream definitions](../../../pkg/bus/streams.go) |
| Shared API/rate/idle logic | [discordapi](../../../internal/discordapi), [discordrate](../../../internal/discordrate), [discordboot](../../../internal/discordboot) |

## Event flow and shared contracts

Gateway reads/heartbeats remain independent of community processing. `Relay`
implements the gateway handler. Unknown event types are dropped by its explicit
subject map rather than guessed; recognized events retain the untouched payload.
`Event.Raw` is a Go byte slice encoded by the shared JSON codec; use the shared
Go contract rather than assuming the outer field is an embedded JSON object.
`GuildID`, `ChannelID` and `UserID` are best-effort extracted routing fields.
`ReceivedAtUnixMs` is ingress arrival time, not later engine processing time.
This prevents a consumer backlog from falsely smoothing raid/join rates.

The six subjects are `discord.ingress.event.{message,member,voice,interaction,audit,guild}`.
Engine owns provisioning `DISCORD_INGRESS`; ingress is publish-only and does not
bind durable consumers. A new event class needs relay mapping, shared subject
constants, stream subjects and engine consumer/handler wiring.

For `INTERACTION_CREATE`, relay first submits a type-5 defer. A failed/malformed
defer drops the event because downstream followup cannot complete unacknowledged
work. Type 6 is inappropriate for the current panel flow: it puts the source
panel into a loading/edit state instead of creating a new response.
Interaction payloads include sensitive webhook tokens; avoid logging raw bodies.

The defer shares Valkey-backed fleet rate coordination with outgress because
Discord's global API budget belongs to the bot token, not to one process.
Presence fetches users counts on a separate RPC connection; publication uses the
BUS connection. Keep those NATS identities/capabilities separated.

## Invariants and pitfalls

- Do not perform welcomes, bans, slash registration or layout setup on this
  gateway receive path; those effects belong in engine/outgress.
- Only the lease holder may dial. Renew errors are tolerated until a 20s deadline
  measured from when the last successful renew was sent; `kept=false` (key owned by
  another replica) demotes immediately. Demotion cancels the session
  and closes the socket with 4000, then returns to standby; the deposed replica must
  not write the checkpoint or `BotStatusKey`. A standby reports Ready/Live while its
  lease loop polls. Rollout/reconnect work must preserve the shared connect budget
  across process restarts.
- Close sockets with 4000 on every path including drain. 1000/1001 end the session
  and the successor's Resume would spend an Identify.
- Each dispatch is published confirmed with message ID `<SessionID>-<Seq>`; a
  successor replaying from an older checkpoint republishes the same IDs, so engine
  dedup (not the broker) absorbs them. An empty SessionID falls back to an
  unconfirmed `PublishJSON` without an ID.
- Prefer Resume when viable; invalid session/fatal close rules and heartbeat
  watchdogs belong to gateway lifecycle, not module logic.
- A fatal close parks the leader, which keeps renewing the lease so the standby
  does not take over and hit the same close.
- Fatal authentication/session failures park/report down; liveness deliberately
  reflects fatal or stalled gateway conditions, unlike a plain process check.
- Empty `DISCORD_BOT_TOKEN` parks idle with health serving through shared boot
  logic. An idle pod is not evidence of a connected Discord bot.
- Main status health feeds engine's vertical report through service token
  `discord-ingress`; keep token, RPC responder and consumers compatible.
- `Relay.publish` is confirmed (`bus.PublishConfirmed`) but never sets `Nats-Msg-Id`;
  delivery is at-least-once and consumers dedup on the message ID.
- Bindings/config/XP/tickets are not ingress state and never belong in local SQL.

## Local verification

Run Go commands from the repository root:

```sh
go test ./app/discord/ingress/...
go test ./app/discord/ingress/internal/gateway ./app/discord/ingress/internal/relay
```

Gateway tests exercise lifecycle, sessions, reconnect, socket writes and budgets;
relay tests cover mapping/defer behavior. Store-backed budget tests may need a
local disposable Valkey binary; inspect their fixture prerequisites.
`go run ./app/discord/ingress` with no token uses the deliberate idle path.
A connected run needs NATS, Valkey and an isolated development bot token; avoid
starting another Identify session for the live fleet token.
