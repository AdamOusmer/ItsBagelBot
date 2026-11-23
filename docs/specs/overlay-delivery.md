# Overlay delivery: decoupling, failover and operations

Date: 2026-11-23
Status: Implemented on branch `feature/overlay-delivery` (not merged). Baseline is `origin/main` at `fda942100`.

## Goal

Visual overlays and the TTS overlay keep working through every gateway deploy, pod
crash, node loss and NATS restart, with these guarantees:

1. No alert is lost while it is younger than its TTL (5 minutes, `pkg/bus/overlays.go:34`).
2. No alert plays twice on the same page, including across reconnects and page reloads.
3. A gateway rollout moves every open overlay to another replica in under 2 seconds,
   with no window where offers find nobody listening.
4. Any replica can serve any overlay. No pod holds state that outlives its own connections.
5. Every failure is visible as a metric before a streamer reports it.

## Current state

### What already works

- The queue lives outside the pods: work-queue streams `OVERLAYS_PREMIUM` and
  `OVERLAYS_STANDARD` with one shared durable per tier, `MaxDeliver -1`, `AckWait` 30 s,
  memory storage, 3 replicas (`pkg/bus/overlays.go:126-137`, `pkg/bus/gossip_jobs.go:24`).
- Every gateway replica pulls from the shared durable (`overlays/puller.ex`) and offers each
  message on `overlays.session.<user_id>` (`overlays/delivery.ex:54`). The pod holding the
  page's SSE stream answers, so delivery already crosses pods.
- TTS acks after playback (`tts/page.ex` `advance`), sends `+WPI` for queued clips every 15 s,
  NAKs unacked clips when its stream dies (`overlays/pending.ex`), and skips duplicate
  `event_id`s in memory (`tts/page.ex:17`).
- Drain flips readiness, pauses pullers, closes streams with 0 to 5 s jitter and answers new
  streams with 503 and `retry-after` (`drain.ex`, `overlay/http.ex`, `tts/http.ex`).
- The only producer today is TTS (`app/gossip/internal/voice/handler.go:235`). Nothing
  publishes visual `overlays.standard.*` alerts yet; the powered badge runs on a page timer.
  The visual lane is therefore cheap to fix now, before visual alerts ship.

### Defects

| # | Defect | Where | Effect |
| --- | --- | --- | --- |
| D1 | Visual stream never monitors its NATS connection | `overlay/events.ex:47` | After a NATS reconnect the stream stays open and deaf. Offers retry until the TTL drops them. |
| D2 | Visual offer is acked when written, not when shown | `overlay/events.ex:90` | A write into a dying connection loses the alert. |
| D3 | Visual `play` frame carries no id | `overlay/events.ex` `@play_frame` | The page cannot skip duplicates, so overlap or redelivery would double play. |
| D4 | Deploys break before they make | `drain.ex:8`, `overlay/events.ex:14`, `priv/powered/page.html:882` | Gap of 3 to 13 s, up to 28 s after a 503. Offers in the gap back off to 30 s (`delivery.ex:13`). |
| D5 | No `preStop`, default 30 s grace, 8 s drain deadline | `deploy/k8s/controls-gateway.yaml`, `drain.ex:9` | Reconnects land on the draining pod while endpoints propagate. |
| D6 | Readiness ignores NATS | `http_handler.ex:15` | A pod with dead NATS planes stays in rotation and serves deaf streams. |
| D7 | In-flight offers run in unsupervised tasks | `overlays/puller.ex:77` | Shutdown kills them mid-offer. The message waits the full 30 s `AckWait` before redelivery. |
| D8 | One unavailable visual check closes the stream | `overlay/events.ex:78` | A short projector outage drops every visual overlay at once. TTS rechecks in 15 s instead. |
| D9 | TTS duplicate set is memory only | `tts/page.ex:17` | An OBS refresh replays the clip that was playing. |
| D10 | Gateway metrics go nowhere | `metrics.ex:5` | The moduledoc names `GET /metrics`, but no route serves it (`http_server.ex` routes). No counter reaches New Relic, so these defects are invisible in production. |
| D11 | 2 replicas on 3 nodes | `controls-gateway.yaml:27` | Each rollout step or node loss moves half of all overlays. |
| D12 | Gateway reads and keys on visitor IP | `overlay_request.ex` `client_ip/1`, `tts/throttle.ex` | Processing the privacy policy does not cover. |
| D13 | Every exchange, stream open and link check is a projector RPC | `overlay/nats.ex`, `tts/nats.ex` | Dead links retrying and post-deploy reconnects hammer the projector. |

## Shared-code findings

AGENTS.md: service code never calls a backend a shared package owns, and a missing operation
is added to the package. The gateway breaks this rule for JetStream and the object store, and
duplicates helpers that `app/twitch/ingress` already has.

| Gateway code today | Problem | Target |
| --- | --- | --- |
| Pull loop on raw `$JS.API.CONSUMER.MSG.NEXT` (`overlays/puller.ex`) | Raw JetStream protocol in `app/` | `Platform.Nats.Pull` in `pkg/elixir/nats`: batch pull, 408 and status handling, re-attach on connection DOWN, pause, supervised handler tasks, `await_idle/1` for drain. |
| `+ACK`, `+WPI`, `+TERM`, `-NAK` strings (`overlays/ack.ex`), ack-subject parser (`overlays/tiers.ex:18`) | Raw ack protocol and subject grammar in `app/` | `Platform.Nats.Ack`: `ack/2`, `progress/2`, `term/2`, `nak/3` (with delay), `subject?/2` (validates against a stream and durable). |
| Object-store reads on raw `$JS.API.STREAM.MSG.GET.OBJ_*` with digest check (`tts/clips.ex:11`) | Raw object-store protocol in `app/` | `Platform.Nats.ObjectStore`: `info/4` (with a per-request timeout) and `stream/3` (chunk iterator that verifies size and SHA-256 before the last chunk reaches the caller). Same semantics as Go `jetstream.ObjectStore`. |
| Hand-rolled request with its own reply subject (`overlays/delivery.ex:54`) | The gateway role may not publish to `_INBOX` (`app/controls/gateway/context.md`), so it subscribes, publishes and unsubscribes by hand, without trace headers | `Platform.Nats.Requester`: one process per plane and reply prefix (here `overlays.session.reply`) holding one wildcard reply subscription, as Gnat does for `_INBOX`, with the caller's trace headers. The 503 no-responders status (`no_responders: true` in `settings.ex`) ends an empty offer at once instead of after 2 s. |
| ETS counters with no reporter (`metrics.ex`) | Same table as `Ingress.Metrics` minus the New Relic flush. `new_relic_agent` is already in the gateway lock through `platform_nats`, so the moduledoc's dependency reason no longer holds. | `Platform.Metrics` in `pkg/elixir/new_relic`: counter table, periodic flush through `Platform.NewRelic.increment/2`, fixed name prefix per service. Ingress and gateway both use it. |
| Jitter math in four places (`ws_handler.ex` resubscribe, `drain.ex`, `puller.ex` retry, `delivery.ex` backoff table) | Same job as `Ingress.Backoff.jittered_ms/4` | `Platform.Backoff` in `pkg/elixir/startup` (no new package), used by both services. |
| `/readyz` checks only the drain flag (`http_handler.ex`) | `Ingress.Health` already does plane checks and aggregation | `Platform.Health` (from `Ingress.Health`, service name as argument) in `pkg/elixir/nats`, since its checks are NATS connections. |
| Own `start/2` and `prep_stop/1` (`application.ex:18`) | `Platform.Startup` exists for this | Use `Platform.Startup.start_link/1` and `prep_stop/3`, as ingress does. |
| Own Ranch listener on hex cowboy 2.21, cowlib 2.22, ranch 2.4 (`http_server.ex`, `mix.lock`) | Ingress runs pinned git builds through `pkg/elixir/cowboy` (moved in this work from cowboy `3a630c0`, cowlib `a3b15e2` and ranch 1.8.1 to the cowboy, cowlib and ranch master commits of 2026-11-19). Two Cowboy builds to patch and tune. | `Platform.Cowboy.Server`. It gains a `loop_routes` option: raw Cowboy loop handlers placed in the same dispatch ahead of the Plug catch-all (`Plug.Cowboy` accepts a custom `dispatch`), so SSE streams keep `cowboy_loop` with hibernation. It also accepts `max_connections: :infinity`, which the gateway uses today, since AGENTS.md forbids fixed internal caps. Page, session, ack and clip routes become one Plug router. |

Stays in the gateway (one user, product-specific): signed cookies, the HMAC ack handle, the
ed25519 device token, `Subjects`, the page HTML. `SignedCookie` and `Handle` both HMAC with
the same key; fold them onto one private `mac/1` inside the gateway.

Inside the gateway, `Overlay.Events` and `Tts.Events` are the same SSE stream with different
frames. They become one `Overlays.Stream` module (see Design). The two page scripts share
their connect, backoff, handoff and duplicate logic in one `priv/overlay/client.js`, inlined
into both pages so the CSP hash scheme stays as it is.

The desktop WebSocket (`ws_handler.ex`, `/v1/connect`) is not shipped and is out of scope. It
moves with the listener unchanged.

## Design

### Ownership

| Part | Owns | Never owns |
| --- | --- | --- |
| Producers (`gossip` voice handler, future visual alert producer) | Event content, `event_id`, `expires_at`, publish through `pkg/bus` | Which pod or page plays it |
| JetStream (`OVERLAYS_*`, `OVERLAYS_MEDIA`) | Unacked alerts and clip bytes until TTL | Session or page state |
| Gateway replica | Its open streams, its in-flight offers | Any alert after it is offered back as NAK or ACK |
| Projector | Link validity, generation, premium | Delivery |
| Page | Playback queue, `seen` set, play acknowledgement | Event ordering across users |

The gateway stays stateless across restarts. Producers keep publishing through
`bus.Overlays.Publish`; no producer knows about gateways, sessions or pages.

### Frame contract

Every alert frame on both pages:

```
id: <event_id>
event: alert
data: {"event_id":"...","kind":"tts|<template>","handle":"<signed ack>","expires_at":"<RFC 3339>","data":{...}}
```

Control frames: `event: ready` (sent once the stream is subscribed to offers), `event: revision`
(visual document changed), `event: reconnect` (`data: {"after_ms": n}`), comment pings every 15 s.

`ready` data is `{"now": <server epoch ms>, "key": "<opaque overlay key>", "revision": n}`. `revision`
is sent on the visual stream only. `key` is the base64url of the first 9 bytes of
`sha256("<kind>:<user_id>:<overlay_id or 'tts'>")`, so it identifies the overlay without exposing ids.

The page opens `<base>/events?v=2` (base `/tts` or `/o`). Without `v=2` the server speaks the legacy
protocol for pages loaded before the deploy, which never reload: TTS sends `event: tts` with the flat
clip JSON including `handle`, visual sends `event: play` with `{}` and acks when it writes. Legacy
streams get no `ready`, `alert` or `reconnect` frames and are closed at drain.

`HEAD <base>/events` mirrors GET: draining is 503 with `retry-after: 5`, a missing NATS plane is 503 with
`retry-after: 1`, an invalid session is 204, no cookie is 401, an unavailable projector is 503 with
`retry-after: 15`, otherwise 200.

Acks carry a reason: `POST /tts/ack` or `/o/ack` with `{"handle": h, "reason": "played" | "duplicate" | "expired" | "dropped"}`.
Progress is `POST /tts/progress` or `/o/progress` with up to 32 handles.

The visual `play` frame is replaced by `alert`. Its `data` carries the template payload.

### Delivery rules

1. A gateway pulls, then offers the message through `Platform.Nats.Requester` on the hub plane with the `overlays.session.reply` prefix (2 s timeout).
2. A stream accepts an offer only if its kind matches (TTS: `tts`; visual: its template) and
   it is not past its handoff close. Accepting means writing the frame and replying
   `{"accepted":true}`, then tracking the ack subject in `Pending`.
3. Nobody accepts (no responders, timeout, or refusal): `-NAK` with delay 250 ms, 1 s, 2 s, 5 s,
   then 10 s for every later delivery. The 5-minute stream `MaxAge` bounds total attempts.
4. The page acks through the existing `POST /tts/ack`, and the new `POST /o/ack` for visual,
   which reuses `Overlays.Acks`:
   - TTS: when playback ends (unchanged).
   - Visual: when the entrance animation starts. A visual alert that started is not replayed.
5. Queued but unplayed TTS clips send `+WPI` every 15 s (unchanged). Visual alerts queue on the
   page the same way and use the same progress call.
6. When a stream ends for any reason, `Pending` NAKs its unacked subjects with no delay, so the
   page's next stream gets them at once.
7. The page drops any alert past `expires_at` and acks it, so a page that opens late never
   plays stale alerts.

### Duplicate handling on the page

The page owns playback state. The server only redelivers until it gets an ack.

- The page keeps `seen`: a map of `event_id` to `expires_at` in `localStorage`, keyed per
  overlay, pruned on load and on every insert. Size stays bounded by the TTL.
- An id is written to `seen` when its frame arrives, before it is queued.
- A frame whose id is in `seen` is acked and skipped.
- So an OBS reload drops everything the page had received, the clip that was playing and the
  queue behind it, and none of it replays. Redeliveries after the reload are acked and skipped.
  Only alerts first received after the reload play.

### Edge rate limit and privacy

The privacy policy names Cloudflare as the party that processes visitor IP addresses
(`web/marketing/src/content/legal/privacy/en/01-data-collected.md:16`) and states we do no
IP-based tracking (`02-data-not-collected.md:11`). Today the gateway reads `CF-Connecting-IP`
(`overlay_request.ex` `client_ip/1`, enabled by `OVERLAY_TRUST_CLOUDFLARE_IP`) and keys an
in-memory map on it (`tts/throttle.ex`). That is removed:

- The overlay `IngressRoute` gets the same pair the dashboard uses
  (`deploy/k8s/console-dashboard.yaml` `dashboard-auth-ratelimit`, `strip-client-ip`):
  - `overlay-session-ratelimit`: `rateLimit` average 10, period 1m, burst 10, keyed on
    `CF-Connecting-IP`, on `/tts/session`, `/o/session` and `/powered/config` only (matched with
    `PathRegexp` so trailing or repeated slashes cannot bypass it). Unlike the old gateway
    throttle, which refunded successful exchanges, Traefik counts every exchange: more than 10
    browser sources starting at once behind one address get a 429 and retry after
    `Retry-After`, which both pages honor.
  - `strip-client-ip` on every overlay route, after the limiter, so no gateway pod sees a
    visitor address. Access logs stay off on this route (`accessLogs: false`, already set).
- The gateway deletes `Tts.Throttle`, `OverlayRequest.client_ip/1` and
  `OVERLAY_TRUST_CLOUDFLARE_IP`. Nothing in the gateway is keyed on a person.
- Traefik holds an address only in its limiter's memory for the window, never logs it and never
  forwards it. This is the same handling the dashboard login already has. The policy text names
  only Cloudflare for IP processing, so the same wording question applies to both routes.

### Resolve cache with automatic invalidation

Streams, exchanges and the 5-minute link checks all call the projector
(`rpc.internal.projector.overlays.resolve`, `.tts.resolve`, `.tts.check`). An OBS page with a
dead link retries its exchange forever, and every reconnect after a deploy re-resolves. A
cache in each gateway pod absorbs that churn, mirroring Go `pkg/cache` (`keyed.go`) and its
feed `pkg/bus/invalidations.go`:

- `Platform.Cache` (new, `pkg/elixir/cache`): ETS table per cache, capacity and TTL per cache,
  single-flight loads per key, a generation counter so a load that raced an invalidation is
  never stored, `invalidate/2`, `invalidate_where/2`, `clear/1`, and a stale-on-error window
  like `cache.StaleOnError`.
- `Platform.Invalidations` (in `pkg/elixir/nats`): subscribes `cache.invalidate.<scope>` (the
  prefix projector publishes with, `app/projector/main.go:66,99`), decodes the Go DTO
  (`broadcaster_id`, `keys`), skips its own `Bagel-Invalidation-Origin`, dispatches
  `{scope, id, keys}` to watchers, and fires the gap callback (clear) whenever its NATS
  connection is replaced, as the Go feed does on reconnect.
- `Platform.Cache.invalidate_on(cache, feed, keys_fun, scopes)` wires the two, as
  `Keyed.InvalidateOn` does.

Gateway caches:

| Cache | Key | Value | TTL | Invalidated by |
| --- | --- | --- | --- | --- |
| Overlay resolve | token hash | overlay or `not_found` | 30 s (projector's `poweredCacheTTL`) | `overlays` scope keys (the projector sends the new and previous token hash, `app/projector/overlay.go:50-55`) |
| TTS resolve | token hash | identity or `not_found` | 30 s | `overlays` scope keys |
| TTS check | `{user_id, generation}` | valid or not | 30 s | `overlays` scope ID (user) |

- Negative entries (`not_found`) are cached too, so a dead link costs one RPC per 30 s per pod
  however often its page retries.
- Unavailable replies are never cached. A stale positive entry is served for up to 5 minutes
  while the projector is unavailable, so a projector outage does not end open streams.
- The NATS user `controls_gateway_rpc` gains subscribe permission on
  `cache.invalidate.overlays` (`deploy/messaging/accounts.yaml`, with its test).
- Ingress moves onto the same modules: `BroadcasterCache` runs on `Platform.Cache` with `refresh_ahead` (an expired entry is served while one background reload runs), `error_ttl_ms` 5 s (the last known lane is kept on a failed load) and capacity 100,000 in place of the idle sweep, and `CacheInvalidator` is replaced by a `Platform.Invalidations` feed on `cache.invalidate.status`. Only the Go DTO is accepted: the legacy `{"all": true}` and bare-id payloads had no publisher in the repo and are dropped, and `"*"` now clears the cache (the old consumer treated it as a literal id).
- `broadcaster_id: "*"` reaches watchers as `id: :all`, which clears the whole cache (the Go feed drops it). The TTS check cache keys on `{user_id, generation}`, so its watcher uses `invalidate_where` on the event's user id.
- The feed fires its gap callback after every attach, so entries loaded while it was detached are cleared once it listens again. Each cache registers with the feed in its own init, and the feed is supervised before the caches (`rest_for_one`).

### Handoff (make before break)

Drain on SIGTERM, after the `preStop` sleep:

1. Set the draining flag. `/readyz` returns 503 and new streams get 503 with `retry-after`.
2. Pause pullers with `Platform.Nats.Pull.pause/1`. Await in-flight offers with `await_idle/2`
   (bounded by the 2 s offer timeout).
3. Send `event: reconnect` to every stream, with `after_ms` uniform in 0 to 1,500.
4. The page waits `after_ms`, opens a new EventSource, and closes the old one when the new one
   delivers `ready`. Readiness already removed this pod, so the new stream lands on
   another replica.
5. During the overlap both streams are subscribed in the same NATS queue group (`tts`, or
   `overlay.<template>`), so each offer reaches one of them. The page skips any duplicate that
   a redelivery still produces.
6. Streams still open 10 s after their reconnect frame are closed with `:fin`. The page's
   ordinary reconnect path then applies.
7. Wait for streams and clip downloads to finish, up to the drain deadline (25 s), then exit.

The same `reconnect` frame is used whenever a stream loses either NATS plane (D1) or must
move for any other server-side reason. Only revocation, link denial and session expiry end a
stream without it.

### Liveness and health

- Every stream monitors both NATS planes (fixes D1).
- A failed visual link check reschedules in 15 s, as TTS does (fixes D8).
- `/readyz` is `Platform.Health`: not draining, both planes connected, all pullers attached
  (fixes D6). `/healthz` stays process-only so a NATS outage never restarts pods.

### Page reconnect policy (both pages)

- First retry 500 ms to 1 s, doubling, cap 15 s, full jitter, reset on the first frame.
- Before each retry the page sends `HEAD <base>/events`. On 200 it opens the stream directly; it exchanges
  its link token again only on 401.
- A 503 with `retry-after` waits at least that long.
- 204 stops for good (link no longer valid), as today.
- The SSE `retry:` field is 1,000 to 3,000 ms (from 3,000 to 8,000).

### Kubernetes

| Setting | Value | Reason |
| --- | --- | --- |
| `replicas` | 3 | One per node. A rollout step moves a third of overlays, not half. |
| `lifecycle.preStop` | sleep 5 s | Endpoint removal reaches Traefik before streams are told to move. |
| `terminationGracePeriodSeconds` | 45 | 5 s preStop + 25 s drain + margin. |
| Drain deadline | 25 s | Fits the overlap close (step 6) plus a full clip download. |
| `maxSurge` / `maxUnavailable` | 1 / 0 | Unchanged. Spread `maxSkew: 1` allows the surge pod (2,1,1). |
| PDB | `maxUnavailable: 1` | Unchanged. |

Capacity is unchanged per replica. No connection cap is added (AGENTS.md).

### Observability

Counters through `Platform.Metrics`, prefix `Custom/ControlsGateway/`:

- `Offer/Accepted`, `Offer/NoResponders`, `Offer/NotConnected`, `Offer/Timeout`, `Offer/Refused`
- `Ack/Ack`, `Ack/Progress`, `Ack/Nak`, `Ack/NakOnClose`, `Ack/Term`, `Ack/Failed`
- `Stream/Opened`, `Stream/Closed/<reason>`, `Stream/Reconnect/<reason>`
- `Page/DuplicateSkipped`, `Page/Expired` (sent as a `reason` field on the ack request)
- `Drain/DurationMs` (incremented by the drain's length), `Drain/StreamsLeft`, `Pull/Attach`, `Pull/Detached` (connection loss or consumer error), `Pull/Paused` (drain), `Pull/Status/<code>`
- `Clip/Served`, `Clip/Interrupted`
- `Cache/Hit`, `Cache/Miss`, `Cache/Stale`, `Cache/Invalidated`, `Cache/Gap` per cache

Alert conditions in New Relic: `Offer/Accepted` falls to zero while `Offer/NoResponders`
rises (deaf gateway), `Ack/Failed` above zero for 5 minutes, `Drain/StreamsLeft` above zero
on any rollout, `Pull/Detached` above zero for 1 minute.

## Operations matrix

Each row is a drill run before release (see Tests). "Delay" is the worst case for an alert
published during the event.

| Event | Expected behavior | Delay | Loss / double |
| --- | --- | --- | --- |
| Rolling deploy | Handoff moves streams. Overlap covers the switch. | under 2 s | none / none |
| Pod SIGKILL or OOM | Pages reconnect on stream error. Pending died with the pod, so its unacked offers redeliver after `AckWait` (30 s). | 30 s | none / skipped by page |
| Node loss | As SIGKILL for that node's pod. Toleration evicts after 60 s, but pages have already moved. | 30 s | none / skipped |
| NATS leaf restart (RPC plane) | Streams get `reconnect` on DOWN. Link checks pause for the outage. Pullers are on the hub plane and keep going. | reconnect time | none / none |
| NATS hub node loss (1 of 3) | R3 memory stream keeps quorum. Gnat reconnects. Streams get `reconnect`, pullers re-attach. | about 5 s | none / skipped |
| Full NATS cluster restart | Memory streams are empty after restart. Queued alerts are lost. Pages reconnect when NATS is back. | n/a | queued alerts lost (accepted risk) |
| Dead link retried by an OBS page | Traefik limits its exchanges. Each pod answers from the negative cache entry, one projector RPC per 30 s per pod. | n/a | n/a |
| Projector outage | New streams get the unavailable frame and retry in 15 to 30 s. Open streams keep delivering and recheck every 15 s. | none for open streams | none / none |
| Edge (Cloudflare) blip | Stream errors, page reconnects with backoff. Pending NAKs at once. | backoff, up to 15 s | none / skipped |
| OBS refresh or scene reactivation | Page reloads. `seen` survives in `localStorage`, so everything received before the reload is dropped, not replayed. | reload time | dropped by design / none |
| Streamer offline, OBS closed | No responders. NAK backoff until the 5-minute `MaxAge` drops the alert. | n/a | dropped at TTL (intended) |
| Two OBS instances on one link | One stream accepts each offer. That alert plays once, on one instance. | none | none / none |
| Link revoked or rotated | Revocation ends the stream with no reconnect. Page gets 204 and stops. | n/a | n/a |
| Cookie key rotation | Old cookies fail, page gets 401, exchanges its link token again. | one exchange | none / none |
| Gateway scaled to zero by mistake | Offers find nobody. Alerts wait up to TTL, then drop. Alert condition fires. | until restored | lost after 5 min |

## Tests

Integration tests on the existing embedded NATS support (`pkg/elixir/nats/test/support`),
through HTTP and SSE only:

1. A visual stream keeps delivering after its NATS connection restarts (D1).
2. A visual alert is redelivered when the stream dies before `/o/ack` (D2).
3. Drain sends `reconnect`, a second stream on another listener receives the next offer, and
   the first closes after the second connects (D4).
4. `/readyz` is 503 while either plane is down, 200 after it returns (D6).
5. Drain waits for an in-flight offer and its ack or NAK before exit (D7).
6. A failed visual check keeps the stream and retries in 15 s (D8).
7. `Platform.Nats.Pull`, `Ack` and `ObjectStore` tests in `pkg/elixir/nats`, ported from the
   gateway's current cases.
8. Page script: duplicate skip, drop-on-reload, `expires_at` drop, handoff close order, backoff
   bounds and `seen` pruning, as table-driven `node --test` cases that load the shared
   `priv/overlay/client.js` in `node:vm` with fake `EventSource`, `fetch`, storage and timers. No
   packages are installed; CI runs Node's built-in runner. The pages had no script test before;
   the existing tests only pin their CSP hashes (`powered_test.exs:18`).

Live drills, before each phase is released: a gateway rollout restart with test alerts on a
channel we own (never a streamer's channel), a pod delete, and a NATS leaf restart. Pass
means `Offer/Accepted` matches alerts published, `Page/DuplicateSkipped` is the only
duplicate signal, and `Drain/StreamsLeft` is zero.

## Phases

Each phase is one PR and ships on its own. They were implemented together on `feature/overlay-delivery`; split them along these lines when opening PRs, and release the manifest changes together with the gateway image that matches them.

1. **Shared packages.** `Platform.Nats.Pull`, `Ack`, `ObjectStore`, `Platform.Health`,
   `Platform.Metrics`, `Platform.Backoff`, `loop_routes` and unlimited connections in
   `Platform.Cowboy.Server`, and the gateway listener moved onto it (hex cowboy 2.21 and ranch 2.4 to
   the pinned master builds, so the whole gateway suite runs on them before merge). Ingress moves to `Platform.Metrics`,
   `Platform.Backoff` and `Platform.Health` in the same PR. The gateway switches to them with
   no behavior change except metrics reaching New Relic. The Containerfile already copies
   `pkg/elixir/nats` and `pkg/elixir/new_relic`; it gains `pkg/elixir/startup` and `pkg/elixir/cowboy`.
2. **Privacy and churn.** Traefik `overlay-session-ratelimit` and `strip-client-ip`, throttle and
   IP code removed, `Platform.Cache` and `Platform.Invalidations`, the three gateway caches,
   the NATS permission.
3. **Gateway liveness.** Plane monitors on both streams, 15 s recheck, readiness with planes,
   supervised offers, `Platform.Nats.Requester` for offers, new NAK delays.
4. **One stream and frame contract.** `Overlays.Stream`, `alert` frames with `id:`,
   `/o/ack`, `Pending` on visual, shared page client with `seen` storage and new backoff.
5. **Handoff and deploy.** `reconnect` frame, drain steps, `preStop`, grace, 3 replicas.
   Released together, since the longer drain needs the longer grace period.
6. **Alert conditions** in New Relic for the counters above.

Every phase updates `app/controls/gateway/context.md`, which documents today's frames, retry
windows, NAK delays and drain order.

## Rejected options

- **Hosted or self-run realtime broker** with history and recovery: adds a service to run.
  JetStream already holds every unacked alert.
- **Per-page cursor replay** (`Last-Event-ID` against a per-user stream): needs a stream per
  user or a replay buffer. The work queue plus ack-after-play gives the same result.
- **Sticky sessions at the ingress:** pins pages to a pod that is about to drain, which is the
  problem being solved.
- **Moving overlays to WebSocket:** SSE already fits one-way delivery, and acks are plain POSTs.
- **BEAM clustering between gateway pods:** `RELEASE_DISTRIBUTION=none` is deliberate
  (`Containerfile`). NATS already routes across pods.

## Settled

- **OBS reload:** the page drops everything it had received and replays nothing (see Duplicate handling).
- **Client IP:** the gateway never receives or processes it. Per-IP limiting happens in Traefik
  only (see Edge rate limit and privacy).
- **Exchange churn:** absorbed by the in-process cache with automatic invalidation. No
  gateway-side throttle remains, so there is nothing to put in Valkey and the gateway gains no
  Valkey dependency.
- **Desktop WebSocket:** not shipped, out of scope.

## Out of scope

- Visual alert producers and templates beyond the frame contract.
- TTS content rules (`docs/specs/tts-safety.md`).
- NATS stream storage changes. `OVERLAYS_*` stay on memory storage with 3 replicas.
