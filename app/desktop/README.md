# ItsBagelBot desktop

A [Wails v3](https://v3.wails.io) app with a Svelte 5 frontend built on
`@bagel/ui`. It currently shows the sign-in screen, using the copy in
`locales/en/console/login.json`. Web links open in the system browser.

Its own Go module, so desktop and CGO dependencies stay out of server binaries.

## Commands

Install the CLI once:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
```

Run from this directory:

```sh
wails3 dev          # live reload window
wails3 build        # bin/ItsBagelBot
wails3 task darwin:package   # bin/ItsBagelBot.app
```

Frontend checks, from `frontend/`:

```sh
bun install
bun run check
```

## OBS overlays

In the existing overlay editor, open **OBS**, select **Publish to OBS**, and
copy the private Browser Source URL into an OBS Browser Source. Match its width
and height to the selected canvas resolution. The desktop app must remain open.
Publishing saves the scene locally and updates connected sources immediately;
editing alone does not change the live source. **Reset private URL** revokes the
old URL while keeping the scene. Replace the URL in OBS after a reset.

The existing editor is available after native device pairing. Its development
preview remains `VITE_SCREEN=shell VITE_SECTION=overlay wails3 dev`.
This change preserves the existing editor and navigation.

### Runtime and isolation

- A Fiber v3 listener binds only to `127.0.0.1:18947`. A fixed port and persisted
  random 256-bit source credential keep the URL stable across restarts. A busy
  port fails startup instead of silently assigning a different OBS URL.
- The OBS bundle is compiled separately into `frontend/obs-dist` and embedded
  independently of the desktop bundle. It reuses the editor's layer rendering
  and card styles, with a transparent canvas and no editor chrome, Wails runtime,
  account credentials, arbitrary HTML, remote assets, or filesystem endpoint.
  This is an origin/API boundary, not an operating-system process sandbox.
- The URL credential lives in its fragment, which is not sent in HTTP URLs or
  referrers. Only the read endpoint receives it, as an Authorization header.
  Exact Host and Origin checks, cross-site request rejection, a restrictive CSP,
  no CORS, and no HTTP write endpoints constrain browser access. Anyone with the
  URL can read the scene from this computer; URL reset revokes future reads and
  clears connected old renderers. Same-user malware is outside this boundary.
- Desktop bindings validate and publish a maximum 256 KiB / 100-layer scene.
  Scenes and credentials are atomically replaced in
  `<os.UserConfigDir()>/ItsBagelBot/obs/overlay.json`. Newly created directories use
  mode 0700 and files 0600 on POSIX; Windows uses the user's profile ACLs.
- Each renderer holds one read request until a publish occurs or 20 seconds
  elapse. Publications wake readers immediately. There are at most 16 waiting
  readers, with bounded HTTP concurrency, body sizes, and timeouts. Reconnection
  retries every two seconds while retaining the last scene. No cloud service,
  database, external network request, video encoding, or per-frame Go work is
  involved.

The current layer model still uses the existing image placeholder and authored
chat/alert content. Live Twitch events, media uploads, and multiple independently
saved scenes are separate features.

### Build and verification

`build` and `build:dev` compile both bundles. `bun run build:obs` rebuilds only
the OBS renderer. Generate Wails bindings before building the desktop frontend
(the Wails task already does this). From `app/desktop`:

```sh
wails3 generate bindings -ts -i
bun run --cwd frontend check
bun run --cwd frontend build
go test -race ./...
go vet ./...
```

Backend tests exercise read authorization, Host/Origin isolation, forbidden
writes, asset MIME types, persistence, invalid scene rejection, bounded readers,
publication wake-up, and revocation of an already waiting reader.

### Package boundaries and allocation budget

`internal/overlay/scene.go` owns the scene contract and validation. `store.go`
owns bounded atomic file persistence behind a small store interface. `source.go`
owns immutable publications and credential rotation independently of Fiber.
`http.go` owns the browser boundary and preloaded renderer assets; `server.go`
owns the Fiber lifecycle and bounded waits. Wails is only a thin adapter in
`overlay.go`. Desktop calls live in `lib/overlay/client.ts`, and the new OBS
controls use the existing shared UI components in `OBSPanel.svelte`. The desktop module imports the repository's `pkg/codec` through a
local module replacement; no overlay code imports a JSON implementation directly.
Fiber is configured with the same codec for its JSON encoder and decoder.

The write path validates in O(layers + messages), builds the response JSON once,
saves the scene, then atomically publishes and wakes readers. The read path has
no JSON encoding, filesystem I/O, scene cloning, or application mutex. Response
bytes and asset bytes are immutable, and wait timers are pooled.

On an Apple M1 Pro, two steady-state benchmark runs measured:

| Path | Time | Bytes/op | Allocs/op |
| --- | ---: | ---: | ---: |
| Full Fiber scene handler, one text layer | 465–487 ns | 0 | 0 |
| Full Fiber renderer document handler | 318–329 ns | 0 | 0 |
| Pooled wait on publication wake | 135 ns | 0 | 0 |

These measure warmed handlers with reused fasthttp contexts, including routing,
headers, authorization and response copying. They exclude socket acceptance,
first-use pool misses, startup, publication/persistence, desktop bindings, and
browser allocations. Regression tests enforce zero steady-state allocations for
scene, document, and JavaScript asset handlers. Reproduce with:

```sh
go test ./internal/overlay -run '^$' -bench . -benchmem
```

The overlay package's functions, including tests, have cyclomatic complexity at
most 10 (`gocyclo -over 10 internal/overlay`).

## Native server connection

The native credential owner and network parser run in separate processes.
`AuthService` and `internal/oauth` keep PKCE state, protected credentials and
DPoP proof creation in the parent. `internal/network` supervises a separate Go
helper that performs TLS, HTTP, WebSocket reception, OAuth JSON parsing and
protobuf decoding inside the required OS sandbox. The helper receives only
bounded private pipes and the configured public origins. There is no direct
HTTP or WebSocket fallback in the parent if the helper cannot run securely.
See [network ownership and limits](internal/network/README.md).

`pkg/websocket` owns bare binary WebSocket/HTTPS transport; `pkg/protobuf` owns
safe optimized protobuf; OAuth JSON uses `pkg/codec`. Native keys use
`pkg/dpop` and `pkg/credential`, and `pkg/sandbox` owns platform isolation.
Application code uses these shared wrappers instead of vendor implementations.
Import guards restrict direct transport fixtures to tests. The parent still
decodes bounded private IPC and protected credential records; it does not parse
raw server HTTP, JSON or WebSocket bodies.

`internal/bridge/protocol/bridge.proto` defines typed chat/alert events and
session controls. `transport.NewClient` requires an injected isolated native
port. It adapts the protocol and creates request-bound DPoP proofs through its
native provider, without owning retry, account or handoff policy. The worker
opens `GET /bridge/v1/events` with the `bagel.bridge.v1` subprotocol and
Authorization/DPoP headers, with no initial account binding or cursor payload.
After session_started, the app sends native HTTPS `POST /bridge/v1/bind` with the
received session ID, applied cursor and migration reference.
`POST /bridge/v1/migration` requests a fresh migration instruction. Both controls
carry typed `application/protobuf` bodies and require 204. Only session_bound on
the event socket authorizes event delivery. Controls reuse healthy HTTPS
connections; each upgraded event socket has its own physical connection. The
app sends no application messages on the WebSocket. Events have no JSON envelope.

`internal/bridge/session` is the single-owner supervisor with fixed-size effects.
It owns primary/pending streams, binding and watchdog deadlines, jittered bounded
retry, and cutover. A candidate promotes only when identity checks pass and the
applied primary cursor reaches the server's cutover boundary. The pending queue
holds at most sixteen owned typed events, with each received event frame capped
at 64 KiB. Buffering transfers a pooled event pointer rather than encoding or
copying it. Duplicates drop, gaps trigger recovery, and the cursor advances only
after the consumer succeeds. Lossless rollout also requires server replay and
sequence guarantees.

Stream recovery defaults to five attempts within 30 seconds, with individual
five-second dial/welcome/bind deadlines and the server's migration window as a
hard limit. EOF/errors and a 45-second activity watchdog detect closure and
silent stalls. Server Keepalive messages should be flushed at least every 20
seconds while otherwise idle. The app sends no application heartbeat. WebSocket
control traffic does not establish origin health. Failed candidates retain a
healthy primary, and retry after an expired migration requests a fresh reference.
Authentication rejection requires sign-in; proxy/transport failures remain
eligible for bounded recovery. The separate process supervisor allows at most
three worker starts per 30-second window. A healthy server handoff does not
restart the worker.

`BridgeService.installNative` accepts a native token/proof provider, a real typed
consumer, and an account-owned native scope from `network.Supervisor.Bridge()`.
Replacement/logout first cancel and join event workers and release outstanding
message leases, then close that scope's sockets and controls. Scope closure
preserves the application-owned OAuth network provider. Consumers borrow a
read-only typed event until Apply returns; they cannot retain, reset or return
it. Cancellation cannot reclaim an event while a consumer is using it. Wails
exposes sanitized status and actions without accepting credentials, endpoints,
proofs or event-consumer installation from the frontend.

Production auth/event origins remain explicit native configuration. No real
production event consumer or server is fabricated, and no discard consumer
advances the event cursor. The compatibility fixtures are separate from a
production event service.

### Native device authorization

The login action starts a native PKCE S256 flow. A temporary Fiber listener
binds an OS-selected port on `127.0.0.1`, and the system browser opens dashboard
`/oauth/desktop/authorize` with state, PKCE challenge and the installation-key
thumbprint. The callback requires exact Host and constant-time state validation,
accepts one result, and copies the code out of Fiber's pooled request storage.
It exposes no tokens, filesystem or event API. It closes after completion,
cancellation or the three-minute approval deadline. Refresh never opens a local
listener or browser; existing devices do not need a dashboard cookie to renew.

Native code exchange, refresh and revocation use the independent Elixir auth
service. The parent signs a DPoP proof for the exact approved request, then the
sandboxed helper sends it with the required token/form fields over verified TLS.
Only bounded parsed OAuth fields return through IPC. Redirects, cookies, HTML
challenges, oversized responses, Bearer token responses and refresh replies that
change the device/grant are rejected. Nonce retries create fresh proofs, and
bounded transient retries cooperate with the issuer's response-recovery window.
The helper necessarily sees credentials sent to that service; the installation
private key never enters the helper.

One protected OS credential item stores the installation private key, rotating
refresh token, pinned token-service origin and grant/device metadata. Access
tokens stay in parent memory. `pkg/credential` uses the operating system's
credential store without a plaintext file or browser-storage fallback. The
software key is not represented as hardware-backed or non-exportable. Restart
refreshes through the pinned auth service before reporting signed in. A paired
device uses native reconnect/renewal retry during an outage instead of opening
another dashboard consent flow.

Refresh calls serialize across native callers. The rotated refresh family is
persisted before the corresponding access token is published; a storage failure
retains the new family in memory for a persistence retry. The app uses the
issuer's expiry metadata and treats sealed tokens as opaque. Renewal starts with
randomized advance time and does not reinstall the event connection. Changing
accounts joins the old account's event workers before replacing its key and
credentials. Lifecycle callbacks run outside token operations, and generations
fence stale ready/stop notifications.

Sign-out removes the local protected record, with a logged-out tombstone if
deletion is unavailable, then attempts DPoP-authenticated remote revocation.
Storage and remote-revocation failures are reported separately. The frontend
receives configured/paired/signed-in/pending state and sanitized errors, never
access tokens, refresh tokens, keys, proofs or callback URLs.

Set `BAGEL_DESKTOP_AUTH_ENDPOINT` to the configured public HTTPS authority of
the independent Elixir auth service; there is deliberately no production
default. `BAGEL_DESKTOP_AUTHORIZE_ENDPOINT` defaults to
`https://dashboard.itsbagelbot.com` for browser consent.
`BAGEL_DESKTOP_BRIDGE_ENDPOINT` supplies the public event-service authority when
its native consumer is available. These values are not Wails inputs. An
unavailable sandbox or unconfigured service leaves remote authorization disabled;
the local OBS renderer has its own independent listener and saved scene.

### Sandbox verification and release requirements

The extra helper process is an OS boundary around server-controlled protocol
parsing. Compromising a server or its parser does not grant the worker general
filesystem, Keychain, parent-memory, process-execution or direct network access.
Its private IPC exposes fixed token, socket and control operations, not an
arbitrary command, file-path or application-update bridge. The parent remains
trusted: it owns the IPC decoder, TCP broker, credentials, UI and overlay writes.
The design does not claim immunity to OS vulnerabilities or compromise of the
trusted parent. The helper can still disrupt its allowed connections; cursor,
replay and recovery policy remain necessary.

On macOS arm64, tests exercised a real ad-hoc signed App Sandbox helper and the
fixed deny-only policy. Tests cover direct Keychain access and the system
`security` CLI, in addition to file, parent-memory, exec and direct-network
denial. See [the Darwin implementation and evidence](../../pkg/sandbox/DARWIN.md).
Windows and Linux implementations were cross-compiled; their kernel enforcement
has not been runtime-verified here. Distribution signing, notarization and
supported-OS validation remain release requirements. In particular, the macOS
temporary SBPL entitlement must be validated with the actual Developer ID
identity and supported OS releases; rejection leaves the feature unavailable.

Build tasks package the separate helper. macOS nests a separately signed
`BagelNetworkWorker.app` under `Contents/Helpers`, then signs the parent from the
inside out. Windows installs `BagelNetworkWorker.exe` beside the app; Linux
packages the static `bagel-network-worker` beside it and requires the platform's
sandbox prerequisites. Never replace a failed helper with an unrestricted one.

### Cloudflare proxy compatibility

The existing public-hostname route uses Cloudflare Tunnel and Traefik. The app
therefore uses binary WebSockets plus typed protobuf. Cloudflare documents
proxied WebSocket support; native Tunnel gRPC is supported through private
subnet routing rather than public hostnames. Origin HTTP/2 does not change that
limitation. Neither the app connection nor the raw TCP broker opens an inbound
server port or advertises an origin IP.
[WebSocket support](https://developers.cloudflare.com/network/websockets/),
[Tunnel gRPC support](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/use-cases/grpc/)

Cloudflare may close sockets during maintenance or idle periods. Origin
keepalives, sequence replay, cursors and bounded recovery remain part of the
contract. An upgrade does not make subsequent message authorization/validation
the edge's job. [Cloudflare lifecycle documentation](https://developers.cloudflare.com/network/websockets/)

Exact fixtures, transport/isolation modes and finite observations are recorded in
[the Tunnel evidence](internal/bridge/TUNNEL_RESULTS.md). The signed macOS sandbox
run on 2026-11-08 delivered eight ordered events through two handoffs and a
150-second heartbeat-only soak, with three expected logical stream attempts and
no unexpected reconnect. System trust worked inside the sandbox, and the second
handoff renewed an expired DPoP nonce. The temporary fixture was removed; no DNS,
Tunnel or firewall changes were required. Historical local transport measurements remain in
[the comparison report](internal/bridge/compare/OPTIMIZED_RESULTS.md).

### Verification and allocation budget

Race tests cover typed transport and supervisor integration, ownership during
cancellation, bounded pending queues, stale callbacks, authentication, token
persistence, account replacement and shutdown. Network tests exercise worker
concurrency, late upgrades, ACK cancellation, bounded framing before decode,
malicious stream floods, handshake rejection and scoped connection ownership.
Import guards enforce shared dependency ownership and prohibit a production
direct transport fallback.

The warmed typed receive/validation/supervisor/consumer dispatch benchmark
measured 460–465 ns/op, 0 B/op and 0 allocations on Apple M1 Pro. It excludes TLS,
wire decoding, IPC, helper startup and native WebSocket receive costs. Those
complete paths are not claimed to be allocation-free. The borrowed account-scope
receive adapter also has a zero-allocation regression check, excluding its
underlying IPC. Pending push/pop are O(1); bounded payload validation is linear
in message bytes.

```sh
# From app/desktop
go test -race ./...
go vet ./...
bun run --cwd frontend check
bun run --cwd frontend build
go test ./internal/bridge -run '^$' -bench BenchmarkRuntimeEventDispatch -benchmem
```
