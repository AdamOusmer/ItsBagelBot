# Twitch outgress context

Agent guide for Twitch delivery and API execution. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for neighboring owners.

## Purpose and boundary

Outgress consumes described Twitch actions and executes authenticated Helix calls.
It owns Twitch token use/refresh coordination, EventSub enrollment, API quotas,
chat delivery, channel-point reward RPCs, stream details and chatter-list RPCs.
Sesame decides command behavior; ingress owns the Conduit sockets. Outgress obtains
the live Conduit ID from ingress and tokens through the token-owning service.
It holds no authoritative account/module/command MySQL persistence.
Moderation actions use the same typed request path as other Sesame outputs;
there is no Rust AutoMod journal/action adapter in this checkout.

## Local nomenclature

- **Outgress message:** typed request in the shared `outgress.Message` contract.
- **Premium/standard lane:** retained incoming priority for ordinary output.
- **System lane:** control jobs such as enrollment and stream status, with a
  longer lifetime/retry budget than perishable viewer-facing chat.
- **Identity (`As`):** app, bot or broadcaster credentials used for an API call;
  the target broadcaster and the credential owner need not be the same account.
- **Grant:** OAuth permission/token relationship; fresh-token rejection can still
  be a permanent scope/moderator/identity failure.
- **Channel registry:** cached broadcaster delivery/enrollment metadata and pause
  control, not the authoritative account table.
- **Quota lease:** a bounded allocation of shared fleet API/send capacity.
- **Batch:** ordered child actions with progress checkpoints; not broker batching.
- **Trial origin:** durable marker that refuses observed-channel outputs before
  their external effects, including batch children.

## Code navigation

| Change | Read first |
| --- | --- |
| Streams, workers, dependencies | [main.go](main.go), [internal/config/config.go](internal/config/config.go) |
| Admission and dispatch | [internal/worker/dispatch.go](internal/worker/dispatch.go): `Process`, `processPayload`; [actions.go](internal/worker/actions.go): `buildActions` |
| API result/retry classification | [internal/worker/execute.go](internal/worker/execute.go): `execute`, `helixResult` |
| Chat, announcements, pins, slash output | [internal/worker/chat.go](internal/worker/chat.go), [pin.go](internal/worker/pin.go), [chat_registry.go](internal/worker/chat_registry.go) |
| Token identity and refresh | [internal/twitch/token.go](internal/twitch/token.go), [broadcaster.go](internal/twitch/broadcaster.go), [internal/tokenstore/tokenstore.go](internal/tokenstore/tokenstore.go), [token_lease.go](token_lease.go) |
| Fleet send coordination | [coordination.go](coordination.go), [internal/worker/buckets.go](internal/worker/buckets.go), [shared ratelimit](../../../pkg/ratelimit) |
| Batches and checkpoints | [internal/worker/batch.go](internal/worker/batch.go), [batch_valkey.go](internal/worker/batch_valkey.go), [batch_jetstream.go](internal/worker/batch_jetstream.go) |
| Enrollment, stream facts, authorization | [internal/worker/eventsub.go](internal/worker/eventsub.go), [streamstatus.go](internal/worker/streamstatus.go), [authz.go](internal/worker/authz.go), [reauth.go](internal/worker/reauth.go) |
| Channel registry/invalidation | [internal/channels/registry.go](internal/channels/registry.go), [pause_jetstream.go](internal/channels/pause_jetstream.go) |
| Management/read RPCs | [rpc/manage.go](rpc/manage.go), [rpc/channelpoints.go](rpc/channelpoints.go), [rpc/streaminfo.go](rpc/streaminfo.go), [rpc/chatters.go](rpc/chatters.go), [rpc/trials.go](rpc/trials.go) |
| Clip completion and facts | [internal/worker/clip.go](internal/worker/clip.go), [clip_verify.go](internal/worker/clip_verify.go) |

## Inputs, output and ownership

[Shared messages](../../../internal/domain/outgress/message.go) declare action type,
broadcaster, sender identity, route/body and trial origin. Workers fill declared
route defaults and dispatch through an immutable [action registry](internal/action).
Default subjects are `twitch.outgress.{premium,standard,system}`.
`main` provisions chat and system streams before consumers;
chat retention is five seconds, system retention five minutes. Chat failures use
one-second paced retries with three redeliveries; control jobs have a longer budget.
Stream migration order prevents overlapping subjects while narrowing old streams.

NATS bus publication and authenticated RPC use separate connections. RPC prefix
is normally `bagel.rpc.outgress`; trial subscription calls are separately wired.
Stream events and authorization-status subjects feed background system behavior.
Chatter reads check the watch-window generation/live session before paging Helix;
stream-info reads fill projected facts. Preserve these admission checks.

Twitch client selects the declared identity, resolves/refreshed credentials and
executes within fleet rate budgets. Registry invalidation updates cached channel
metadata. Optional Valkey startup does not promise every dependent operation can
continue safely without Valkey: examine each action's fallback/fail-closed contract.

Clip creation is a compound flow: parse the accepted clip ID, send the chat
reply, publish `data.twitch.clip.created` for Discord, then schedule asynchronous
publish verification. The fact precedes that verification in current code; do
not assume it proves the clip is already fully published. A chat-reply failure
currently returns before fact publication.

## Invariants and pitfalls

- `rejectTrialOutput` blocks durable `Origin=trial` requests before dispatch,
  including batches. It intentionally performs no per-send Valkey membership
  lookup; tagged origin is the current executor boundary.
- API 429/5xx produce retryable errors. Permanent 4xx are dropped; 401/403 after
  token refresh require reauthorization/moderator correction, not repeated sends.
- Outgoing floor checking lives in Sesame; this executor has no shared
  `replysafety` package or final generic-Helix text guard. Account for that actual
  boundary when adding new producers or completion replies.
- Rate limits are fleet-coordinated; do not replace them with independent pod
  counters or assume one chat rate bucket covers every Helix endpoint.
- A batch checkpoints completed children; changing checkpoint/lease ordering
  can repeat external side effects during redelivery.
- Incoming AutoMod assessment/enforcement lives in Sesame's Go pipeline.
  There is no `OUTGRESS_AUTOMOD_ENABLED` setting, Rust action stream or journal
  claim/result protocol on main; do not document those as existing guarantees.
- Do not infer replay safety from publication UUIDs; current bus contracts matter.
- Service health is probed by Sesame for the Twitch vertical; maintain health
  token `outgress` compatibility when touching names or health RPCs.

## Local verification

Run Go commands from the repository root:

```sh
go test ./app/twitch/outgress/...
go test ./app/twitch/outgress/internal/worker -run 'Trial|Batch|Stream'
go test ./app/twitch/outgress/rpc
```

Integration fixtures may require a disposable Valkey/NATS process; inspect their
skip conditions and avoid treating missing dependencies as a passing integration.
`go run ./app/twitch/outgress` requires configured NATS, token RPCs and Twitch
credentials and may execute pending actions; use isolated development services.
