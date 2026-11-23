# Controls gateway context

Read [shared language](../../../CONTEXT.md) and the [app map](../../../CONTEXT-MAP.md)
for neighboring owners.

## TTS browser source beta

OBS opens `https://overlay.itsbagelbot.com/tts#<token>`. The token is 32 random
bytes encoded as unpadded base64url. The page holds the token in memory after
clearing the fragment and sends it in JSON `POST /tts/session`, retrying
network errors, 429 (`Retry-After`) and 5xx with jittered exponential backoff
and stopping only on 404. Keep the token out of paths, queries and logs.
Session, event and clip responses are uncached.

Overlays owns token hashes and generations. The gateway calls
`rpc.internal.projector.overlays.tts.resolve` with the lowercase SHA-256 hex of
the encoded token on the RPC leaf. Successful exchange sets the host-only
`bb_tts` cookie with `user_id`, `generation` and `exp`, signed using HMAC-SHA256.
The cookie is HttpOnly, Secure, SameSite=Lax and scoped to `/tts` for 30 days.
Exchange limiting is Traefik's job (a per-address limit on the session routes); the gateway never reads or keys on a client address.

`GET /tts/events` checks the session generation when opening and every five
minutes via `rpc.internal.projector.overlays.tts.check`. Only a definite `false`
(or an expired cookie) closes the stream; an unavailable check keeps it and
rechecks in 15 seconds. It subscribes to `controls.tts.revoked.<user_id>` on the
RPC leaf so it can close immediately on revocation. Event requests register with
the shared drain before opening the stream. A stream starts with a jittered SSE
`retry:` field of 1 to 3 seconds, then `event: ready` once it is
subscribed, and receives a comment every 15 seconds while idle. When the projector
check is unavailable, the response sends a retry of 15 to 30 seconds and ends.
When either NATS plane has no connection (a restart in progress) the response is
`503` with `Retry-After: 1`. `HEAD <base>/events` mirrors GET without a stream: draining is 503 with
`Retry-After: 5`, a missing NATS plane 503 with `Retry-After: 1`, a bad session 204, no cookie 401, an
unavailable projector 503 with `Retry-After: 15`, otherwise 200. The page opens streams as
`<base>/events?v=2`. Without `v=2` the stream speaks the legacy protocol for pages loaded before
the deploy (TTS `event: tts` with the flat clip JSON, visual `event: play` with `{}` and the old ack at write,
a 3 to 8 second `retry:`, no `ready`, `alert` or `reconnect` frames) and is closed at drain after the same
jitter. Drain rejects new `GET /tts/events`
requests with `503` and `Retry-After: 5`; the order is in the Drain paragraph below.

Clips travel through the acked, shared queue the worker provisions: work-queue
streams `OVERLAYS_PREMIUM` and `OVERLAYS_STANDARD` (subjects
`overlays.<tier>.<kind>.<user_id>`) with one durable pull consumer each,
`overlays-premium` and `overlays-standard`, shared by every gateway pod. Each
pod runs one `Platform.Nats.Pull` loop per tier on the hub connection
(`overlays.ex`, started with the server). For every message the handler
parses the user ID and kind from the subject and sends a core request to
`overlays.session.<user_id>.<kind>` (on no responders it retries once on the legacy `overlays.session.<user_id>`, kept for one release) with `{"ack", "kind", "data", "delivered", "expires_at"}`
(`expires_at` is the JetStream publish time plus the 5 minute TTL). The reply
subject is `overlays.session.reply.<instance>.<nonce>` from `Platform.Nats.Requester`, because the gateway role may not
publish to `_INBOX`. Every open
`GET /tts/events` stream of that user subscribes to the subject of its own kind (`tts`, or the visual template) and to the legacy per-user subject in a NATS queue group (`tts` for TTS streams, `overlay.<template>` for visual ones), so two sources on one link receive each offer once, including during a handoff overlap. The one
`Overlays.Stream` module serves both kinds: a TTS stream accepts offers of kind `tts`,
a visual stream those whose kind equals its template. It tracks the ack subject in
a monitor process, writes the frame below and replies `{"accepted": true}`:

```
id: <event_id>
event: alert
data: {"event_id","kind","handle","expires_at","data":{...}}
```

`data` is the event (TTS: `clip_key`, `event_id`, `kind`, `duration_ms`, `expires_at`,
optional `trace_id`), `handle` the signed ack handle, and `expires_at` the event's own
`expires_at` when it has one, else the offer's. Without a reply the loop sends `-NAK` (a missing responder, NATS 503, naks at once instead of waiting out the 2 second offer timeout) with a
delay of 0.25, 1, 2, 5, then 10 seconds by delivery count; an invalid message gets
`+TERM`. The gateway only binds to streams and consumers by name.

The handle is `base64url(ack subject).base64url(HMAC-SHA256(cookie key,
"<user_id>\n<ack subject>"))`. `POST /tts/ack {"handle", "reason"?}` and
`POST /tts/progress {"handles": [...up to 32]}` need the session cookie and a
JSON body, verify each handle for the session user and that the subject is an
ack subject of the two overlay consumers, then publish `+ACK` or `+WPI`. The
visual page uses the same handler at `POST /o/ack` and `POST /o/progress` with the
`bb_o` cookie (which carries the `user_id`). `reason` is `played`, `duplicate`,
`expired` or `dropped` (the page could not render the alert); any other value is 404. Every ack
counts `Ack/Ack`, and the last three also count `Page/DuplicateSkipped`, `Page/Expired` or `Page/Dropped`. A bad
handle is 404, no session is 401, success is 204, a failed publish is 503 and counts `Ack/Failed`. Each stream keeps the ack
subjects it sent for five minutes in a monitor process and publishes `-NAK`
for them when the stream ends, so another session or pod gets
the clip at once. A stream that already sent its `reconnect` frame and is then closed by the client publishes `+WPI` instead (`Ack/ProgressOnHandoff`), because the old page may still be playing the clip and its own ack settles it. An offer for a kind with no open stream gets no responders and is requeued at once. Acking an already acked message is a no-op.

`GET /tts/clip/<event_id>` streams a WAV from the `OVERLAYS_MEDIA` Object Store
bucket with `Content-Length` from the object metadata size, using the cookie
user ID and the event ID as the object key (`<user_id>/<event_id>`). Missing
and expired objects return 404. The page plays clips one at a time in arrival
order and aborts stuck clips after `duration_ms + 5 s`. It acks a clip when it
ends or fails twice. Outside OBS the sound hint appears when autoplay
is blocked. In OBS the hint stays hidden and audio pauses while the source is
inactive or hidden.

Both pages run the shared client `priv/overlay/client.js`, inlined into each
page's single `<script>` at compile time (`@external_resource`, so the CSP
hashes cover it). It has no DOM access and takes its browser objects as
arguments, so `test/js/client.test.js` drives it with fakes
(`node --test app/controls/gateway/test/js/`, run in CI). It owns:

- Connecting. Every connect, first or after any `EventSource` error (the client closes the stream and does not leave the retry to the browser), starts with
  `HEAD <base>/events`. A 200 opens `<base>/events?v=2` directly. A 401 with a
  held token runs `POST <base>/session` (404 stops for good; network errors,
  429 and 5xx retry) and then opens the stream. A 204 stops for good. Any other
  answer, including 503, retries and waits at least `Retry-After`. Retries wait
  500 ms to 1 s, doubling to a 15 s cap with full jitter, and reset on the first
  frame of any kind. Streams without `?v=2` get the legacy protocol for pages
  loaded before the deploy.
- Handoff. On `event: reconnect` (`{"after_ms": n}`) it waits `n` ms, opens a
  second stream and closes the old one when the new one sends `ready`. If the
  old one errors first, the normal reconnect applies. If the new one fails first,
  it is retried with backoff while the old one lives.
- Ready. `event: ready` carries `{"now", "key", "revision"}`. The client keeps
  `now` minus its own clock as an offset for every `expires_at` comparison, keys
  `seen` by `key` (an unchanged key keeps the store, a changed key carries its entries over), and passes `revision` (visual only) to `onRevision`.
- Duplicates. `seen` maps `event_id` to its expiry in `localStorage` under
  `bb_seen:<key>` (`<base>` before the first `ready`). It is re-read and merged
  on every check and insert, so two sources of one link share it. Expired
  entries are pruned on load and on every insert. An
  id is written when its `alert` frame arrives, before it is queued, so an OBS
  reload replays nothing it had received. A frame whose id is in `seen` is acked
  with reason `duplicate` and skipped. A frame past `expires_at` is acked with
  reason `expired` and skipped.
- Delivery. Accepted alerts queue and reach the page one at a time through
  `onAlert(alert, {started, done})`. `started()` acks `played` (`POST <base>/ack`,
  three tries 1 s apart, 200, 204, 401 and 404 settle it). `done()` advances the
  queue without acking, so an alert the page could not show stays unacked. An
  item past `expires_at` when it reaches the head is acked `expired` and skipped.
  Handles still queued and not started are posted to `POST <base>/progress` every 15 seconds in batches of at most 32.

`event: revision` (`data: <n>`) reaches the visual page only. `event: ready` is sent once
the stream is subscribed: `{"now": <epoch ms>, "key": "<opaque overlay key>"}`, plus `"revision": n` on the
visual stream so the page can sync after a gap. `key` is the base64url of the first 9 bytes of
`sha256("<kind>:<user_id>:<overlay_id or 'tts'>")`.

## Overlays

OBS opens `https://overlay.itsbagelbot.com/o#<token>`. `/powered#<token>` serves
the same page and uses the same endpoints, so existing sources keep working.
The token is 32 random bytes as unpadded base64url. The page holds it in memory,
clears the fragment and posts it once as JSON to `POST /o/session`. The gateway
sends the lowercase SHA-256 hex of the token to
`rpc.internal.projector.overlays.resolve` on the RPC leaf and sets the signed
`bb_o` cookie (`overlay_id`, `generation`, `token_hash`, `user_id`, `exp`; HttpOnly, Secure,
SameSite=Lax, path `/o`, 30 days). `not_found` is 404, any other refusal or a
missing responder is 503. Keep
the token out of logs.

`GET /o/doc` re-resolves the cookie's hash and returns
`{"document", "revision", "premium"}` uncached. No cookie or an expired one is
401. A rotated link or a changed generation is 403 `not_found`. An overlay the
account may not show is 403 `premium_required`. Projector sets `allowed` and
withholds the document when it is false, so the premium rule lives there.
`GET /o/events` is SSE through the same `Overlays.Stream` module as TTS. It opens only
when the same check passes (otherwise 204), re-checks every five minutes
(`overlay_check_ms` overrides it), sends `event: revision` when the revision moves and
closes when the generation changes, the overlay is no longer allowed or the session
expires. An unavailable check keeps the stream and reschedules after 15 seconds
(`overlay_recheck_ms`), clamped to the session expiry like every check.
`HEAD /o/events` answers 200, 204 or 503 from the same check, which the page uses
before reopening. It registers with the shared drain like TTS events. Each stream
also subscribes to `overlays.session.<user_id>.<template>` (the resolve reply's `user_id`), so it
receives only offers whose kind matches the overlay's template. Delivery is
at-least-once like TTS: it writes the `alert` frame, tracks the ack subject, replies
`{"accepted": true}` and leaves the message unacked until the page posts `/o/ack`
when the entrance animation starts. A stream that dies first NAKs its unacked
subjects at once (`Ack/NakOnClose`). `overlays.standard.powered.<user_id>` shows Powered by Bagel on that
account's open sources.

`GET /o` and `GET /powered` serve `priv/powered/page.html`, read at compile time.
The page holds exactly one `<style>` and one `<script>` tag without attributes,
(the shared client is inlined into that script at the `{{overlay_client}}` line) and the CSP carries their SHA-256 hashes, `img-src` and `media-src` for
`https://cdn.itsbagelbot.com`, and `connect-src 'self'`. The script selects a
template module by `document.template` for system overlays. `powered` is the only
one. Custom overlays show a placeholder notice. Only the dashboard origin may
frame the page. `powered_dev_frame_ancestors` adds localhost origins for
dashboard development. `GET /powered/assets/<file>` serves only the `.webp` and
`.woff2` files present in `priv/powered/assets` at compile time, cached as
immutable.

## Desktop WebSocket

The desktop handler `ws_handler.ex` and its tests remain in the tree.
`GET /v1/connect` and the desktop bridge are unmounted in the beta. The
integration harness can mount the desktop WebSocket in isolation.

The desktop handler verifies `v1.<base64url(payload)>.<base64url(signature)>`
over the ASCII bytes of the first two segments. The Ed25519 key comes from
`CONTROLS_TOKEN_PUBLIC_KEYS`. Users owns token signing and device grants.
The verified token supplies the device and user IDs.

The hub carries core NATS device events, commands and takeover messages over
`controls.device.<device_id>.evt`, `.cmd` and `.takeover`. A newer connection
publishes its randomly generated connection ID to displace an older one.
`controls.revoked.<device_id>` closes that device's connection. The handler
hibernates between messages and resubscribes when Gnat reconnects.

Desktop close codes are 4000 for expiry, 4001 for takeover, 4003 for
revocation, 1001 for graceful shutdown, 1007 for malformed frames and 1009
for frames over 64 KiB. Draining marks readiness unavailable before closing
open desktop and TTS SSE connections.

## Code map

| Change | Read first |
| --- | --- |
| Listener and beta routes | [http_server.ex](lib/controls_gateway/http_server.ex), [router.ex](lib/controls_gateway/router.ex), [events_handler.ex](lib/controls_gateway/events_handler.ex) |
| Graceful drain | [drain.ex](lib/controls_gateway/drain.ex) |
| Browser page and token exchange | [page.ex](lib/controls_gateway/tts/page.ex), [http.ex](lib/controls_gateway/tts/http.ex) |
| Session and caches | [session.ex](lib/controls_gateway/tts/session.ex), [caches.ex](lib/controls_gateway/caches.ex) |
| Event streams (TTS and visual) | [stream.ex](lib/controls_gateway/overlays/stream.ex), [events_handler.ex](lib/controls_gateway/events_handler.ex) |
| Clip downloads | [clips.ex](lib/controls_gateway/tts/clips.ex) |
| Queue pull loops and acks | [overlays.ex](lib/controls_gateway/overlays.ex), [delivery.ex](lib/controls_gateway/overlays/delivery.ex), [acks.ex](lib/controls_gateway/overlays/acks.ex), [handle.ex](lib/controls_gateway/overlays/handle.ex) |
| Projector TTS RPC | [tts/nats.ex](lib/controls_gateway/tts/nats.ex) |
| Powered page, assets and config | [powered/http.ex](lib/controls_gateway/powered/http.ex), [powered/page.ex](lib/controls_gateway/powered/page.ex) |
| Projector overlay RPC | [overlay/nats.ex](lib/controls_gateway/overlay/nats.ex) |
| Leaf and hub routing | [Platform.Nats.Planes](../../../pkg/elixir/nats/lib/platform/nats/planes.ex) |
| Listener and credentials | [runtime.exs](config/runtime.exs), [controls-gateway.yaml](../../../deploy/k8s/controls-gateway.yaml) |

Production uses the `controls-gateway` Doppler project for the shared
`OVERLAY_COOKIE_KEY` (at least 32 bytes) and the dedicated
`controls_gateway_rpc` NATS credentials. Traefik forwards the overlay hostname
to the HTTPS listener. The gateway receives no client address.

Projector answers are cached per pod (`caches.ex`, `Platform.Cache`): overlay resolve
and TTS resolve by token hash (any invalidation for a user clears the whole TTS resolve cache, since the projector names only the new hash on rotation), TTS check by `{user_id, generation}`, each for 30
seconds with up to 4096 entries. Dead links are cached as `not_found`, so a page
retrying one costs one projector request per 30 seconds per pod. An unavailable
reply is never stored, and an entry up to 5 minutes past its TTL is served while the
projector is down. `Platform.Invalidations` listens on `cache.invalidate.overlays`
(RPC leaf): the event's keys drop resolve entries, its `broadcaster_id` drops that
user's check entries, `*` clears every cache, and a NATS reconnect clears them too. The same feed (`overlays/link_watch.ex`) sends every open stream of that user (all streams on `*` or a reconnect) a link check after a 0.1 to 1 second jitter, so a rotated or revoked link ends without waiting for the five minute check.
The projector publishes an `overlays`-scope invalidation on TTS changes too (`app/projector/tts.go`). `allowed` and `premium` ride in the cached overlay, so a premium change reaches open streams and documents up to 30 seconds late (5 minutes if the projector is down). A visual stream keeps running when its check is unavailable and rechecks in 15 seconds. Counters are `Cache/Hit`, `Miss`, `Stale`, `Invalidated` and `Gap`, suffixed with the cache name.

The listener is `Platform.Cowboy.Server`: the Plug router serves every route except the two SSE streams and the desktop WebSocket, which stay Cowboy loop routes. Counters go through `Platform.Metrics` (prefix `Custom/ControlsGateway/`) to New Relic. `/readyz` is a `Platform.Health` report: not draining, `:gnat_jetstream` and `:gnat_rpc` connected, and both overlay pullers attached (503 otherwise); `/healthz` only proves the process. Both SSE streams monitor both NATS connections and end with `Stream/Closed/nats_down` (hub) or `rpc_down` (leaf) when either drops, counting `Stream/Opened` and `Stream/Closed/<reason>`. Offers that find no connected requester count `Offer/NotConnected`; a failed `-NAK` or `+TERM` publish counts `Ack/Failed`. Drain (`drain.ex`) runs in this order: set the draining flag (`/readyz` 503, new streams 503 with `Retry-After`); pause the pullers (the inbox subscription stays, so a message already on the wire is NAKed with a short delay instead of waiting out `AckWait`, counted as `Pull/PausedNak`; the subscription is dropped once the drain's idle wait returns or 1 second after the pause); await both tiers' in-flight offers concurrently (at most one 2 second offer timeout); send every registered SSE stream `{:reconnect, after_ms, :drain}` with `after_ms` uniform in 0 to 1,500; then poll until every registered stream, desktop socket and `/tts/clip` download has finished or the deadline passes (`drain_deadline_ms`, 25 seconds). Streams, sockets and downloads register in `ControlsGateway.Connections` with their kind; desktop sockets keep their own jittered `shutdown_close`. It counts `Drain/DurationMs` (elapsed milliseconds) and `Drain/StreamsLeft` (SSE streams still open at the deadline). Its last step flushes the counters to New Relic. While draining, Plug responses on HTTP/1.1 carry `Connection: close`. The overlay `IngressRoute` services use `nativeLB: false` so Traefik balances over pod endpoints and stops sending new requests to a terminating pod during the preStop window.

A stream told to reconnect (drain, or a NATS plane going down) writes `event: reconnect` with `data: {"after_ms": n}` and counts `Stream/Reconnect/<reason>` (`drain`, `nats_down`, `rpc_down`). It keeps accepting offers so the page can overlap the old and new stream, and closes when the client does or when the forced close fires: 10 seconds after a drain reconnect (`stream_force_close_ms`), after a plane `DOWN` whose `after_ms` is uniform in 0 to 1,500: `after_ms` plus 1 second when the hub drops (`stream_down_grace_ms`, the stream can no longer receive offers) and the full 10 seconds when the leaf drops (`rpc_down`, it can still deliver). Closes are counted as `Stream/Closed/<reason>`, and `Stream/Closed/client` when the client closed first. Revocation, link denial and session expiry still end the stream with no reconnect frame.

`Platform.Nats.Planes` starts `:gnat_rpc` on the node-local leaf
(`NATS_RPC_HOST`, `NATS_RPC_PORT`) and `:gnat_jetstream` on the hub (`NATS_HOST`,
`NATS_PORT`). Both use the gateway credentials and verify TLS against their
own host. The RPC connection serves Projector requests and carries
TTS revocations. The hub connection carries the overlay queue, `OVERLAYS_MEDIA`,
`overlays.session.<user_id>.<kind>` and desktop subjects. The clusters are separate, so a hub request for a leaf RPC
gets no responders. Build the gateway image from the repository root so the
Containerfile can copy the shared package.
