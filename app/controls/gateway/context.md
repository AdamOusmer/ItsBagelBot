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
the encoded token. Successful exchange sets the host-only `bb_tts` cookie
with `user_id`, `generation` and `exp`, signed using HMAC-SHA256. The cookie is
HttpOnly, Secure, SameSite=Lax and scoped to `/tts` for 30 days. Failed attempts
are limited per client IP on each gateway replica.

`GET /tts/events` checks the session generation when opening and every five
minutes via `rpc.internal.projector.overlays.tts.check`. It subscribes to
`controls.tts.revoked.<user_id>` so it can also close immediately on revocation.
Its ordered ephemeral consumer filters `OVERLAY_TTS` to
`overlay.tts.<user_id>`. The SSE ID is the stream sequence. A reconnect
starts after `Last-Event-ID` or the `after` cursor sent by a replacement
`EventSource`. With neither cursor the consumer receives new events.
Each event carries `clip_key`, `event_id`, `kind`, `duration_ms` and
`expires_at`. When the originating event carries a trace, its ID is also in
`trace_id`. Event requests register with the shared drain before opening
the stream. A stream starts with a jittered SSE `retry:` field of 3 to 8
seconds and receives a comment every 15 seconds while idle. When the initial
NATS or Users RPC check is unavailable, the response sends a retry of 15 to 30
seconds and ends. Drain rejects new `GET /tts/events` requests with `503` and
`Retry-After: 5`. It closes open streams cleanly with the shared drain jitter.

`GET /tts/clip/<event_id>` streams a WAV from `TTS_CLIPS` with `Content-Length`
from the object metadata size, using the cookie user ID and the event ID as the
object key. Missing and expired objects return 404. The transparent browser
source deduplicates `event_id` with a bounded set, queues audio in arrival order,
skips expired events, and aborts stuck clips after `duration_ms + 5 s`. Outside
OBS the sound hint appears when autoplay is blocked. In OBS the hint stays
hidden and audio pauses while the source is inactive or hidden.

On `EventSource` `CLOSED`, the page backs off with jitter and uses
`HEAD /tts/events` to check the same session without opening a stream. A 204
ends the retry loop and clears queued audio. Otherwise the page renews the
session with any held token and reopens the stream.

The worker provisions both JetStream resources in `CONTROLS_GATEWAY_RPC`.
`OVERLAY_TTS` is a file-backed stream with a five-minute max age.
`TTS_CLIPS` is a three-replica file-backed Object Store bucket with a
five-minute TTL. The gateway creates ephemeral readers for events and reads
Object Store metadata and chunks through the stream message API. Missing
resources leave readiness available.

## Powered by overlay

OBS opens `https://overlay.itsbagelbot.com/powered#<token>`. Overlays owns the
token, 32 random bytes as unpadded base64url, stored in plaintext and
rotatable. The page posts it once as JSON to `POST /powered/config`. The
gateway forwards the token to `rpc.internal.projector.overlays.powered.resolve`
on `:gnat_rpc` and replies `{"config": ..., "premium": bool}` uncached. A
`not_found` refusal is 404, any other refusal or a missing responder is 503.
Failed attempts share the TTS per-IP throttle. Keep the token out of logs.

`GET /powered` serves `priv/powered/page.html`, read at compile time. The page
holds exactly one `<style>` and one `<script>` tag without attributes, and
the CSP carries their SHA-256 hashes. Only the dashboard origin may frame it.
`powered_dev_frame_ancestors` adds localhost origins for dashboard
development. `GET /powered/assets/<file>` serves only the `.webp` and `.woff2`
files present in `priv/powered/assets` at compile time, cached as immutable.

## Desktop WebSocket

The desktop handler `ws_handler.ex` and its tests remain in the tree.
`GET /v1/connect` and the desktop bridge are unmounted in the beta. The
integration harness can mount the desktop WebSocket in isolation.

The desktop handler verifies `v1.<base64url(payload)>.<base64url(signature)>`
over the ASCII bytes of the first two segments. The Ed25519 key comes from
`CONTROLS_TOKEN_PUBLIC_KEYS`. Users owns token signing and device grants.
The verified token supplies the device and user IDs.

Core NATS carries device events, commands and takeover messages over
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
| Projector TTS RPC | [tts/nats.ex](lib/controls_gateway/tts/nats.ex) |
| Powered page, assets and config | [powered/http.ex](lib/controls_gateway/powered/http.ex), [powered/page.ex](lib/controls_gateway/powered/page.ex) |
| Projector RPC | [powered/nats.ex](lib/controls_gateway/powered/nats.ex) |
| Listener and credentials | [runtime.exs](config/runtime.exs), [controls-gateway.yaml](../../../deploy/k8s/controls-gateway.yaml) |

Production uses the `controls-gateway` Doppler project for the shared
`OVERLAY_COOKIE_KEY` (at least 32 bytes) and the dedicated
`controls_gateway_rpc` NATS credentials. Traefik forwards the overlay hostname
to the HTTPS listener. The gateway takes the client IP from the Cloudflare
header on this route, and defaults to the socket peer locally.

The gateway holds two NATS connections with the same credentials. `:gnat` goes
to the hub (`NATS_HOST`), where `OVERLAY_TTS` and `TTS_CLIPS` live. `:gnat_rpc`
goes to the node-local leaf cluster (`NATS_RPC_HOST`), where Projector serves the
overlay resolve and check RPCs and publishes revocations. The two clusters are
not bridged, so an RPC sent to the hub gets no responders.
