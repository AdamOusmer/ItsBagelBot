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
Failed attempts are limited per client IP on each gateway replica.

`GET /tts/events` checks the session generation when opening and every five
minutes via `rpc.internal.projector.overlays.tts.check`. Only a definite `false`
(or an expired cookie) closes the stream; an unavailable check keeps it and
rechecks in 15 seconds. It subscribes to `controls.tts.revoked.<user_id>` on the
RPC leaf so it can close immediately on revocation. Event requests register with
the shared drain before opening the stream. A stream starts with a jittered SSE
`retry:` field of 3 to 8 seconds and receives a comment every 15 seconds while
idle. When the initial NATS or Users RPC check is unavailable, the response sends
a retry of 15 to 30 seconds and ends. Drain rejects new `GET /tts/events`
requests with `503` and `Retry-After: 5`. It stops the pull loops first, then
closes open streams cleanly with the shared drain jitter.

Clips travel through the acked, shared queue the worker provisions: work-queue
streams `OVERLAYS_PREMIUM` and `OVERLAYS_STANDARD` (subjects
`overlays.<tier>.<kind>.<user_id>`) with one durable pull consumer each,
`overlays-premium` and `overlays-standard`, shared by every gateway pod. Each
pod runs one pull loop per tier on the hub connection
(`overlays/puller.ex`, started with the server). For every message the loop
parses the user ID from the subject and sends a core request to
`overlays.session.<user_id>` with `{"ack", "data", "delivered"}`. The reply
subject is `overlays.session.reply.<nonce>`, because the gateway role may not
publish to `_INBOX`. Every open
`GET /tts/events` stream of that user subscribes to that subject, writes an
unnamed `data:` frame (the default `message` event, which pages loaded
before this protocol also read) whose JSON is the event (`clip_key`, `event_id`,
`kind`, `duration_ms`, `expires_at`, optional `trace_id`) plus a `handle`,
and replies `{"accepted": true}`. Without a reply the loop sends `-NAK` with a
delay of 0.5, 2, 5, 10, then 30 seconds by delivery count; an invalid message gets
`+TERM`. The gateway only binds to streams and consumers by name.

The handle is `base64url(ack subject).base64url(HMAC-SHA256(cookie key,
"<user_id>\n<ack subject>"))`. `POST /tts/ack {"handle"}` and
`POST /tts/progress {"handles": [...up to 32]}` need the session cookie and a
JSON body, verify each handle for the session user and that the subject is an
ack subject of the two overlay consumers, then publish `+ACK` or `+WPI`. A bad
handle is 404, no session is 401, success is 204. Each stream keeps the ack
subjects it sent for five minutes in a monitor process and publishes `-NAK`
for them when the stream ends for any reason, so another session or pod gets
the clip at once. Acking an already acked message is a no-op.

`GET /tts/clip/<event_id>` streams a WAV from the `OVERLAYS_MEDIA` Object Store
bucket with `Content-Length` from the object metadata size, using the cookie
user ID and the event ID as the object key (`<user_id>/<event_id>`). Missing
and expired objects return 404. The page queues clips in arrival order, skips
an `event_id` it already received (acking the redelivery), and aborts stuck
clips after `duration_ms + 5 s`. It acks a clip when it ends, fails twice, is
skipped or the server returns 404, and posts progress for every queued or
playing clip every 15 seconds. Outside OBS the sound hint appears when autoplay
is blocked. In OBS the hint stays hidden and audio pauses while the source is
inactive or hidden.

On `EventSource` `CLOSED`, the page backs off with jitter and uses
`HEAD /tts/events` to check the same session without opening a stream. A 204
ends the retry loop and clears queued audio. Otherwise the page renews the
session with any held token and reopens the stream.

## Overlays

OBS opens `https://overlay.itsbagelbot.com/o#<token>`. `/powered#<token>` serves
the same page and uses the same endpoints, so existing sources keep working.
The token is 32 random bytes as unpadded base64url. The page holds it in memory,
clears the fragment and posts it once as JSON to `POST /o/session`. The gateway
sends the lowercase SHA-256 hex of the token to
`rpc.internal.projector.overlays.resolve` on the RPC leaf and sets the signed
`bb_o` cookie (`overlay_id`, `generation`, `token_hash`, `exp`; HttpOnly, Secure,
SameSite=Lax, path `/o`, 30 days). `not_found` is 404, any other refusal or a
missing responder is 503. Failed attempts share the TTS per-IP throttle. Keep
the token out of logs.

`GET /o/doc` re-resolves the cookie's hash and returns
`{"document", "revision", "premium"}` uncached. No cookie or an expired one is
401. A rotated link or a changed generation is 403 `not_found`. An overlay the
account may not show is 403 `premium_required`. Projector sets `allowed` and
withholds the document when it is false, so the premium rule lives there.
`GET /o/events` is SSE. It opens only when the same check passes (otherwise 204),
re-checks every five minutes (`overlay_check_ms` overrides it), sends
`event: revision` when the revision moves and closes when the generation
changes, the overlay is no longer allowed or the session expires. It registers
with the shared drain like TTS events.

`GET /o` and `GET /powered` serve `priv/powered/page.html`, read at compile time.
The page holds exactly one `<style>` and one `<script>` tag without attributes,
and the CSP carries their SHA-256 hashes, `img-src` and `media-src` for
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
| Listener and beta routes | [http_server.ex](lib/controls_gateway/http_server.ex) |
| Graceful drain | [drain.ex](lib/controls_gateway/drain.ex) |
| Browser page and token exchange | [page.ex](lib/controls_gateway/tts/page.ex), [http.ex](lib/controls_gateway/tts/http.ex) |
| Session and rate limit | [session.ex](lib/controls_gateway/tts/session.ex), [throttle.ex](lib/controls_gateway/tts/throttle.ex) |
| Event and clip streams | [events.ex](lib/controls_gateway/tts/events.ex), [clips.ex](lib/controls_gateway/tts/clips.ex) |
| Queue pull loops and acks | [puller.ex](lib/controls_gateway/overlays/puller.ex), [delivery.ex](lib/controls_gateway/overlays/delivery.ex), [acks.ex](lib/controls_gateway/overlays/acks.ex), [handle.ex](lib/controls_gateway/overlays/handle.ex) |
| Projector TTS RPC | [tts/nats.ex](lib/controls_gateway/tts/nats.ex) |
| Powered page, assets and config | [powered/http.ex](lib/controls_gateway/powered/http.ex), [powered/page.ex](lib/controls_gateway/powered/page.ex) |
| Projector RPC | [powered/nats.ex](lib/controls_gateway/powered/nats.ex) |
| Leaf and hub routing | [Platform.Nats.Planes](../../../pkg/elixir/nats/lib/platform/nats/planes.ex) |
| Listener and credentials | [runtime.exs](config/runtime.exs), [controls-gateway.yaml](../../../deploy/k8s/controls-gateway.yaml) |

Production uses the `controls-gateway` Doppler project for the shared
`OVERLAY_COOKIE_KEY` (at least 32 bytes) and the dedicated
`controls_gateway_rpc` NATS credentials. Traefik forwards the overlay hostname
to the HTTPS listener. The gateway takes the client IP from the Cloudflare
header on this route, and defaults to the socket peer locally.

`Platform.Nats.Planes` starts `:gnat_rpc` on the node-local leaf
(`NATS_RPC_HOST`, `NATS_RPC_PORT`) and `:gnat_jetstream` on the hub (`NATS_HOST`,
`NATS_PORT`). Both use the gateway credentials and verify TLS against their
own host. The RPC connection serves Projector requests and carries
TTS revocations. The hub connection carries the overlay queue, `OVERLAYS_MEDIA`,
`overlays.session.<user_id>` and desktop subjects. The clusters are separate, so a hub request for a leaf RPC
gets no responders. Build the gateway image from the repository root so the
Containerfile can copy the shared package.
