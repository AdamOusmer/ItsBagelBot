# $(urlfetch) — Implementation Plan

<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->

`$(urlfetch)` lets non-technical broadcasters call third-party HTTP APIs from custom chat commands: a definition — URL, optional JSON path, optional API key — is authored once in the console, and any command response embeds a `{urlfetch:name}` token where fetched data should render. Four product requirements pin the design: **non-technical editor with rehearsal** — structured form, JSON view, and a server-side rehearsal that renders the real response before save; **easy and safe API keys** — stored in the commands service sealed with Tink AEAD, never echoed back, never pasted into command text; **isolated sandboxed fetching** — all user-defined fetches execute in gossip behind an SSRF gate enforced in Go before any dial, never on sesame's zero-alloc hot path; **hidden egress IPs** — user fetches leave through Cloudflare WARP so an annoyed target cannot null-route the three hot-path nodes carrying Twitch, NATS, and everything else. The threat model treats every broadcaster as an untrusted author: they can aim us at cluster-internal or metadata addresses, hand us secrets to keep, and attract upstream abuse verdicts against whatever IP range we dial from.

## Where it plugs in

- **Sesame expansion**: pre-resolve slot beside counters — `runCustom` (app/sesame/engine/dispatch.go:74, `bumpCounterTokens` :100), `expandCommand` (app/sesame/engine/vars.go:52), `tokens` struct (vars.go:15-25).
- **Commands storage**: ent entity conventions (app/commands/ent/schema/commands.go:24-68), dashboard verbs (app/commands/rpc/dashboard.go:28-35), Valkey projection (internal/projection/valkey.go:244,:514; client.go:296).
- **Gossip execution**: Builder (app/gossip/internal/provider/builder.go), transports (app/gossip/internal/core/http.go:122,:157-170), per-subject fleets (app/gossip/internal/engine/engine.go:34-47), provider registration (providers/all.go).
- **Console**: commands page patterns (routes/(app)/commands/+page.svelte), shared validation (console/shared/lib/commands-validate.ts:73).
- **Importer**: urlfetch tags recognized-but-unmapped today (console/shared/lib/importer/moobot.ts:287,300).

## Phase 0 — Decisions & syntax

Locked before any code. These are contracts between commands, gossip, sesame, and the console.

| Decision | Choice | Rejected alternative | Why |
|---|---|---|---|
| Shape | Named definitions; `{urlfetch:name}` resolves a stored, reviewable definition | Inline URLs in the command text | Raw URLs in chat templates are invisible to review, can't carry keys, and re-parse per message; a named def is one auditable object with one console surface |
| Ownership | Definitions + keys in the commands service: new ent entities beside `Commands` (Twitch `user_id` keying), keys sealed with Tink AEAD | Modules service, or gossip-owned storage | commands already owns per-broadcaster command data and the console CRUD rides it; schemas stay isolated per service |
| Execution home | A provider inside gossip via the standard Builder | A dedicated fetcher microservice, or fetching inside sesame | gossip already dials external hosts with client/cache/rate-limit plumbing shared; sesame's hot path must never await an upstream |
| Egress | WARP as a SOCKS-proxy sidecar in gossip's pod (`warp-cli mode proxy`, localhost:40000, service-token enrollment) | Cloudflare Workers; Zero Trust Gateway on the nodes | Workers splits fetch logic into a second codebase and meters requests; Gateway-on-nodes takes the default route, pushing ALL cluster egress (Twitch, NATS, Doppler, New Relic, image pulls) through one tunnel whose breakage takes down the bot, and user URLs can't be enumerated into split-tunnel rules |
| Trust default | Inverted `.Trusted()`: user-defined fetches egress via the WARP proxy by default; direct cluster-IP egress requires an explicit `.Trusted()` builder flag | Trusted-by-default, or opt-in proxy | The safe path must be the default path — a forgotten flag must fail toward hidden egress, not toward exposing production IPs. The flag selects egress only; the SSRF gate runs before the dial on both paths |
| Token grammar | `{urlfetch:name}` rides the existing brace grammar: `normalizeKey` (app/sesame/module/vars.go:66) lowercases the name before the first `:` and leaves payload untouched, exactly as `{choice:Hi,Yo}` behaves | A second sigil or a pre-expansion pass | Brace-with-colon plus unknown-key passthrough already works; two syntaxes double the parsing for zero capability |
| Quotas | Per-broadcaster caps, all configurable (proposed defaults: 20 definitions, 500 fetches/day — placeholders, see follow-ups) | Unlimited, or fleet-global limits only | A broadcaster-authored fetch is unbounded abuse surface by construction; caps bound the worst case per broadcaster instead of fleet-wide |

**Cloudflare economics (verified Aug 2026):** Zero Trust Gateway/SWG is $0 up to 50 seats with full SWG included; pay-as-you-go is $7/user/mo billed annually; dedicated egress IPs are a Contract-plan add-on only — so egress is shared Cloudflare IPs either way, which is acceptable because the goal is hiding *our* IPs, not uniqueness. Workers for comparison: Free 100k req/day at 10ms CPU, Standard $5/mo incl 10M req/mo — shelved because the WARP sidecar keeps 100% of the logic in Go, rides unmetered fair use, and is one fewer codebase, at the cost of an always-on tunnel pod and a NET_ADMIN-privileged sidecar.

## Phase 1 — definitions & keys (commands service)

Everything lands in the commands service (`app/commands`) — definitions and sealed keys live where custom commands live, so `{urlfetch:name}` resolution costs gossip no cross-service hop except the one key RPC.

### Schema

New `app/commands/ent/schema/fetchdefinition.go` + `fetchkey.go`, in the house style of `commands.go` (immutable `user_id` :26, `created_at`/`updated_at` defaults :65-67, normalize hook :78-99):

**FetchDefinition**
- `user_id` Uint64 `.Immutable()` — Twitch ID only, schemas isolated per service (commands.go:17-18)
- `name` String NotEmpty MaxLen(32) — stored bare/lower-case like the trigger (hook :78-99)
- `url` String MaxLen(512)
- `json_path` Strings Optional — JSON array of segments, alias-style (:34), depth ≤8
- `key_label` String Optional MaxLen(32) — names the FetchKey used
- `is_active` Bool Default(true); unique `(user_id, name)` (:73-74)

**FetchKey** (shape of `GoveeCredential`, app/modules/ent/schema/goveecredential.go)
- `user_id` Uint64 `.Immutable()` (:27); `label` String MaxLen(32); `key_enc` Bytes `.Sensitive()` (:32); `last4` String len 4 — derived at seal time, displayable forever; timestamps; unique `(user_id, label)`

**Soft-delete: none.** Commands rows hard-delete and ride `Deleted:true` on the event (data_events.go:65; "deletions are immediate", commands.go:111-114). Both entities follow.

**Referential integrity: application-enforced, zero ent edges — recommended.** (a) the existing schema declares no edges; (b) edits run write-behind through a coalescing batcher (commands.go:119,:262), so enqueue-time row existence ≠ flush-time existence and an FK turns a flush-window race into a wedged retry loop (the failure `flush` already defends against per-item, commands.go:575-585); (c) command→definition lives inside free-text response, unexpressible as a constraint. Deliberate consequences: deleting a key leaves dangling `key_label`s — fetches fail closed with "key missing" until relinked; deleting a definition is gated by the referenced-by check below; gossip consumes the projected copy, which no DB cascade reaches anyway.

### Sealing

Envelope is `domain.SecureEnvelope{Ciphertext, AttachedData}` (Packing.go:6-9) via `crypto.NewCrypto(keysetJSON)` (aead.go:23), `Pack`/`Unpack` (aead.go:38,:50). AAD binds broadcaster **and** label: `fetchAAD(userID, label)` = `<uid>|fetch_key|<label>`, mirroring `goveeAAD` (app/modules/repository/govee.go:157-161) and its binding test (govee_test.go:103) — the label term stops an envelope copied onto another label of the same user from opening. Write-only: `SetKey` packs, stores, derives last4, drops plaintext; no read path returns values. **Rotation stance:** one keyset per service (users/main.go:113; modules mount deploy/k8s/modules.yaml:171), no per-row version column — rotation is an offline re-seal job over rows, never a wire-format change.

### Internal key RPC

Prefix `env.Get("NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.commands.fetchkey")` — the exact convention of govee (app/modules/rpc/govee.go:43) and spotify (spotify.go:43). One verb `.get` (goveekeys.go:39 shape):

```go
// internal/domain/rpc/fetchkey
type KeyGetRequest struct { UserID, Label string }
type KeyGetReply   struct { Key, Error string } // omitempty; empty+empty = none on file
```

Mirrors `goveerpc.KeyGetRequest/Reply` (internal/domain/rpc/govee/govee.go:50-60). Served via `bus.QueueSubscribeJSON[...]` with the standard 2s queue budget (dashboard.go:39). Consumer timeout **2s**, same reasoning as goveekeys.go:17-20 — the service answers from its own row plus one Unpack, no upstream hop. **Failure contract:** plaintext is used for the single upstream call and never cached (goveekeys.go:24); a transport failure surfaces as a chat-reply error, never a silent skip; an Unpack failure (AAD mismatch = corruption/tamper) is terminal and logged with user_id+label only, never ciphertext or plaintext.

### Dashboard verbs

Extend the dashboard verb table (rpc/dashboard.go:28-35) under the existing prefix (`NATS_COMMANDS_SUBJECT_PREFIX`, app/commands/main.go:137):

- **fetch_list** — defs `{name,url,json_path,is_active,key_label}` + keys `{label,last4,created_at}`. Values never appear.
- **fetch_set_def** — upsert incl. rename via OriginalName (dashboard.go:96-101 shape); validated and quota-counted synchronously, written immediately (not write-behind — see Quotas).
- **fetch_set_key** — seals, stores, replies `{last4}` once: derived from the just-submitted plaintext, then persisted, so later lists show label+last4 with no decrypt.
- **fetch_delete** — definition delete scans that user's commands for `{urlfetch:<name>}` in `response` and refuses, naming the referencing commands, unless forced; key delete always allowed (dangling labels fail closed).
- **Audit trail** — one zap line per mutate (op, user_id, name/label; never the value), the discipline of the publish-error log (commands.go:611-617).

### Validation & quotas

Rules live in internal/domain/validate beside CommandName/Aliases/Response (validate.go:101,:125,:156) as `errors.Is` sentinels, shared with the console through reply `Error` strings (dashboard.go:105):

| Rule | Value |
|---|---|
| FetchDefName | `^[a-z0-9_]{1,32}$` (save + rename) |
| FetchURL | https-only, ≤512 chars; host denylist re-checked at fetch time |
| Host denylist | IP literal (`net.ParseIP`), `localhost`, `.local`, `.internal` — rejected at save AND fetch |
| FetchPath | segments `[A-Za-z0-9_-]+`, depth ≤8 |
| KeyLabel / KeyValue | ≤32 / ≤512 |
| Defs per broadcaster | 20 |

The URL must also pass the immovable IP-logger floor (`grabify`-class hosts; floor_test.go:41, wired as `validate.CheckFloor` at floor_test.go:20). **Quota enforcement point:** the synchronous fetch-def upsert runs `COUNT(*) WHERE user_id=?` before insert — deliberately not the write-behind path, whose `Upsert` enqueues with no row check at all (commands.go:251-265); a cap there is unenforceable until flush. Per the repo's comment convention, every new numeric cap ships with a measurement/why-alternatives-failed comment — none exists yet; owed for: 20 defs (chat-expand worst case), 512 URL chars (signed query strings), depth 8, and the two-layer denylist (catches saved-before-blocklist entries and post-save DNS changes).

### Projection

Definitions join the per-user settings hash gossip already reads (`cache.UserKey("commands:", uid)`, valkey.go:247): one `fetch:<name>` field per def holding the full view JSON (url, path, key_label, is_active), beside `command:<name>` (:249). New `SetFetch(es)`/`GetFetches` mirror SetCommands/SetCommandsTTL/GetCommands (valkey.go:405-436,:514) with a `fetch:projected` marker (:525). Invalidation rides a new `FetchChangedDTO` + `SubjectFetchChanged = "data.commands.fetch_changed"` beside SubjectCommandChanged (data_events.go:14,:52), carrying `Deleted` (:65) so rename/delete retire fields via SetCommand's HDEL path (valkey.go:256-260). TTL: DefaultTTL 24h (valkey.go:29). Read side: `Client.FetchDefs` clones `Client.Command` (client.go:296-329) — short-TTL in-process entry per def with negative caching (:46), tier-2 Valkey, tier-3 projector-RPC fallback (client.go:322-327 shape). **Keys never enter Valkey or any cache** — the projection carries only `key_label`; plaintext stays one-call-per-fetch via the RPC above.

### Provisioning deltas

deploy/k8s/commands.yaml mounts nothing for Tink today (volumes :214-220 are nats-client-tls + health-tls; secrets arrive via envFrom `commands-env` :180-182, itself synced by the DopplerSecret :4-17). Deltas, copying modules.yaml verbatim where possible:
1. Doppler: add `TINK_KEYSET` to the commands project — picked up on the `resyncSeconds: 60` tick (commands.yaml:17); use a `processors:`/`asName` rename only if needed, per the pattern at deploy/cache/dopplersecrets.yaml:30-33.
2. Manifest: `TINK_KEYSET_PATH=/etc/tink/keyset.json` env (modules.yaml:171-172), `tink-keyset` mount at `/etc/tink` (:193-194), optional secret volume mapping key `TINK_KEYSET` → `keyset.json` (:234-240).
3. Startup: best-effort, modules-style — unset path or absent file disables key custody (warn; `fetch_set_key`/delete answer "key custody unavailable"; definitions still work keyless), present-but-invalid keyset is fatal (govee.go:47-66). This diverges from users' fatal `MustGet` (users/main.go:113) on purpose: commands is on the core path even with zero keys sealed.

## Phase 2 — gossip: the urlfetch provider

New package `app/gossip/internal/providers/custom/` plus changes in `app/gossip/internal/provider` and `app/gossip/internal/core`.

### `.Trusted()` + the `b.Client` chokepoint

`provider.NewProvider(name, d).Trusted()` returns `*Builder` (chain off builder.go:47-50) and marks every client this provider constructs as trusted-direct; the default is **inverted** — unmarked providers construct WARP-lane clients. `b.Client(base, headers, timeout)` replaces every `core.NewHTTPClient` call: same `HTTPClient` (core/http.go:157-170), transport chosen by the flag, each construction recorded in a `[]clientSpec{lane}` on the Builder. `.Trusted()` panics if a client was already constructed through that builder — trust is positional by construction, so the one-line idiom cannot be misordered. Core's constructor becomes unexported (`newHTTPClient`): afterward the only route to a direct-egress client lives inside core, so a bypass means adding a new export to core/http.go — a reviewable diff, not a silent one.

**Boot-time validation, honestly.** `Build` extends its panic-at-boot contract (builder.go:61-69): Validate errors when a provider constructs zero clients yet declared `.Trusted()` (dead flag), and logs one line per provider — `"govee: 1 client (trusted)"`. It **cannot** introspect Handle closures — bespoke handlers capture services by closure (provider.go:15-17) — so a provider could still smuggle a raw `http.Client`. The chokepoint narrows that to a deliberate act (hand-rolling `net/http`), which is the strongest check that doesn't rebuild every provider's internals. Runtime complement: WARP-lane external segments (the core/http.go:207 pattern) carry `lane=warp`, so wrong-lane egress is visible in the trace that owns the whole picture.

**Migration is mechanical.** Nine providers, fourteen call sites, one `.Trusted()` line each, in registration order (providers/all.go:36-44): urchin (urchin.go:120), hypixel + mojang (hypixel.go:110-111), mcsr (mcsr.go:135), paceman base+user (paceman.go:127-128), fortnite shop+stats (fortnite.go:225-226), govee (govee.go:120), clashroyale (clashroyale.go:77), valorant base+content (valorant.go:196,199), spotify api+accounts (spotify.go:160-161). The trusted lane IS today's behavior (`sharedTransport`), so the diff is provably neutral — core/http_test.go must stay green untouched.

### SSRF gate before every dial, both lanes

One `core.SSRFCheck(*url.URL) error` invoked in `Do`'s request build (before the segment at core/http.go:193-215); microsecond cost, and it guards trusted-lane misconfig too. Scheme https only — http rejected at **save** time (for author feedback) **and re-checked at fetch time** (old stored defs must not silently upgrade). Port 443 only. Host must be a DNS name: bare IP literals rejected outright (kills `127.0.0.1`/`169.254.169.254` literal forms); denylisted suffixes rejected (`localhost`, `*.local`, `*.internal`, `.svc`, `.svc.cluster.local`). Redirects via `CheckRedirect`: cap 3 hops, re-run the full gate on each `Location` host, forbid https→http downgrade (typed error).

**The rebinding tradeoff, stated.** On the WARP lane the hostname travels inside the SOCKS CONNECT and resolves at Cloudflare's edge — we cannot pre-verify the resolved IP. Mitigations in order of strength: (1) the Zero Trust Gateway policy blocks RFC1918/link-local/loopback at the CF edge, so a rebound private IP dies outside our pod; (2) the host-string denylist catches obvious names; (3) the trusted lane is config-owned hosts and gets the same host-shape gate for uniformity. Resolving locally then dialing by IP through SOCKS would defeat remote-DNS and hand CF's edge bare IPs its policy handles inconsistently — accepted tradeoff.

**Body and time budgets.** 1 MiB cap via manual `io.LimitReader(resp.Body, 1<<20+1)`, error if filled — not `http.MaxBytesReader` (request-shaped, 413-flavored error), matching core/http.go:259's precedent; since net/http decompresses gzip transparently, the limit lands **post-decompression — that is the gzip-bomb guard**, and Content-Length is never trusted. Content-type allowlist `application/json` + `text/*`. Total fetch budget **2.5s** inside the endpoint's declared 3s `Timeout` (builder.go:139; engine generic default 5s, engine.go:29), leaving 500ms for decode/extraction/buckets/marshal. Rationale: 3 permitted hops × ~700ms connect+TTFB via the WARP edge is the realistic worst case; per-hop deadlines derive from remaining wall clock, never a fixed per-request timeout.

### `custom.fetch`

A bespoke Handle endpoint, served at `bagel.rpc.gossip.custom.fetch` (engine.go:49-76), not a byte-flow: the base URL varies per request and the flow skeleton caches by identity alone (flow.go:179-205 binds one fetch per endpoint — a URL-varying fetch would leak channel A's answer into channel B's entry). Declaration mirrors govee's bespoke shape (govee.go:105-107).

Fields joining `gossiprpc.Request`: `DefID string`; inline `Def *FetchDef` (rehearsal path for the dashboard's try-it button, never persisted); `ChannelID`/`UserID` (gossip.go:28); `IsPremium` riding the reserved bucket lane (gossip.go:31); `DryRun bool` (execute, spend no bucket, write no cache); `Fresh bool` (skip the positive-cache read, still write). `DefID` resolves just-in-time through a commands-service twin of the govee key resolver (core/goveekeys.go:25-47): same `bus.RequestJSONTimeout` (pkg/bus/rpc.go:85), same 2s bound (goveekeys.go:20), same one-call-no-retention contract. Reply: `{Status: ok|denied|limited|upstream_error|timeout|bad_def, Values []string, MS int}`, Values capped server-side at 5 × 256 chars (<4KB reply) — callers never see raw bodies.

**Extraction and caching.** Server-side dot-path extraction (`$.data.items[0].name`) over the sonic-decoded body — indexing and string coercion only, no JSONPath dependency. Results cache through `core.CachedBytes` under `gossip:custom:fetch:<hash(def_id)>`; friendly 400/404 negatives take the existing negative path (cache.go:285-287) at **15s**; infrastructure failures stay uncached (cache.go:288).

**Budgets and the breaker.** Three `core.Buckets` layers re-keyed per caller via `WithKey` (buckets.go:80-83), exactly as govee re-keys per broadcaster (govee.go:311-313): per-channel 6/min (bounded by chat cadence), per-definition 30/min, per-host 120/min fleet-shared (bounds abuse); premium spends only the general bucket (buckets.go:97-105), standard keeps the 75% split (buckets.go:65-75). Breaker on store primitives: 5 consecutive connect/timeout failures arm `store.SetNX("gossip:custom:cb:<host>", …, 60s)` — the fleet-wide mutual-exclusion primitive (cache.go:31-35), claimed pod-locally then authoritative fleet-wide like SWR refresh (cache.go:297-313) — and armed hosts answer typed `UpstreamError{429, LocalDeny}` (buckets.go:103 semantics) until expiry.

## Phase 3 — WARP sidecar & egress split

### `warpTransport`

Second transport beside `sharedTransport` (core/http.go:54): `DialContext` = `proxy.SOCKS5("tcp", "127.0.0.1:40000", nil, proxy.Direct)` from `golang.org/x/net/proxy` (promote indirect dep to direct). Hostname passes through CONNECT, so DNS resolves remotely at CF's edge — sparing this lane the ndots search-domain junk the pod's dnsConfig tuned away (deploy/k8s/gossip.yaml:120-145). Carried over unchanged: `ForceAttemptHTTP2` (TLS still terminates at the **origin** through the tunnel, so ALPN negotiates h2 normally) and both h2 health constants `SendPingTimeout: 15s` / `PingTimeout: 3s` (core/http.go:75,84,147-150) — a dead tunnel is indistinguishable from a dead direct conn, and the pairing argument stands (lower either alone and the blackhole returns, core/http.go:104-119). `IdleConnTimeout: 10min` carried; `MaxIdleConns/PerHost` kept as h1-fallback sizing (h2-inert as today, core/http.go:124-133). Pooling stance: net/http keys proxied connections by (proxy, target host), so tunnels still pool **per-origin** through the single localhost proxy — no cross-origin reuse, no new exhaustion mode. Failure mode, fail closed: sidecar down = instant connect-refused on loopback surfaced as typed `ErrWARPDown`; **no fallback to direct transport, ever** — the untrusted lane's guarantee is that direct egress is impossible, so a dead sidecar degrades to `limited` replies, not unproxied dials.

### Manifest

Extends deploy/k8s/gossip.yaml:

```yaml
initContainers:
- name: warp
  image: docker.io/cloudflare/cloudflare-warp:<ver>@sha256:<digest> # hand-pinned digest, bumped deliberately
  restartPolicy: Always                          # native sidecar, k8s >=1.29
  securityContext:
    runAsNonRoot: false                          # warp-svc needs root for the tun device
    capabilities: {add: ["NET_ADMIN"], drop: ["ALL"]}
    devices: [{devicePath: /dev/net/tun}]        # k8s >=1.30 securityContext.devices;
                                                 # else fall back to a scoped privileged run
  envFrom: [{secretRef: {name: gossip-warp-env}}] # DopplerSecret wired like gossip-env
                                                  # (gossip.yaml:32-45), renames via
                                                  # processors (dopplersecrets.yaml:30-34)
  ports: [{name: warp-socks, containerPort: 40000}]
  readinessProbe: {tcpSocket: {port: warp-socks}, periodSeconds: 2, failureThreshold: 15}
  livenessProbe: {exec: {command: ["warp-cli","--accept-tos","status"]}, periodSeconds: 30}
  resources:
    requests: {cpu: 50m, memory: 64Mi}
    limits: {cpu: 500m, memory: 256Mi}
```

Bootstrap: `warp-svc &` → `warp-cli --accept-tos mode proxy` (binds the loopback listener **before** enrollment completes) → `warp-cli --accept-tos enrollment token "$WARP_ENROLL_TOKEN"` (Zero Trust service token from Doppler) → `warp-cli --accept-tos connect`. Startup ordering follows: the socks port binds early, readiness passes, and gossip serves immediately — trusted providers never wait on WARP, and untrusted requests during the enrollment window fail closed per-request. Resources are sized separately from gossip's 32Mi/128Mi + GOMEMLIMIT=96MiB envelope (gossip.yaml:197-198,224-230; engine.go:42-46): the daemon holds its own tunnel state and must not compete for gossip's headroom.

### Rollout

Hand-pinned digests applied with kubectl per repo convention (nothing rewrites these manifests): change both pins in deploy/k8s/gossip.yaml in one commit and apply; `maxSurge: 0 / maxUnavailable: 1` rolls one replica at a time (gossip.yaml:60-64) under the PDB (gossip.yaml:261-270), and required anti-affinity spreads the three WARP daemons across nodes (gossip.yaml:90-95).

### NetworkPolicy/Cilium stance — stated honestly

Pod-level policy cannot separate the lanes: Cilium (deploy/cilium/values.yaml) sees gossip's direct HTTPS to known FQDNs plus ONE UDP flow to Cloudflare standing for the entire untrusted lane — indistinguishable at pod level from a compromised process doing the same, and blind inside the tunnel. The actual boundaries are the Go chokepoint (unexported core constructors ⇒ bypass = reviewable diff), the SOCKS listener bound to in-pod loopback (no other workload can ride it), and the CF Gateway policy killing RFC1918 at the edge. Worthwhile defense-in-depth anyway: a Cilium egress FQDN allowlist of the trusted hosts — static config, breaks nothing, partially contains a fully-compromised binary.

**Cost and failure:** three Zero Trust seats (one per enrolled replica), well inside the free ≤50-seat tier — $0. WARP down fails the untrusted lane closed with typed errors while every trusted provider continues unaffected; WARP degradation costs latency only.

## Phase 4 — sesame integration

### Token scan

Custom responses gain a third pre-expansion scan beside counters. `urlFetchNames(tmpl)` mirrors `counterTokenNames` byte-for-byte: fast-path `strings.Contains(response, "{urlfetch")` (the `bumpCounterTokens` guard at dispatch.go:241), then `strings.Index` on `{urlfetch:` + `IndexByte '}'`, reusing the same brace-key grammar `module.Expand` parses later (module/vars.go:26-42). Names are normalized through the same fold counters use (`NormalizeCounterName`, applied at engine/vars.go:68) and deduped in first-appearance order, so a template repeating one definition resolves once. No regex — the counter precedent is deliberate zero-alloc scanning and it stays that way.

### Pre-resolve fan-out

New `fetchUrlTokens(ctx, c, response)` sits next to `bumpCounterTokens` in `runCustom` (dispatch.go:100), returning `map[string]string`. Distinct names fan out concurrently (bounded goroutines, errgroup-style: first error cancels the batch, per-token result recorded either way). Same-name repeats collapse at the scan; cross-command single-flight is *not* added — gossip's own cache is the shared flight, and a second caller arriving mid-fetch gets the cached bytes microseconds later. The call goes through the existing `GossipCaller` surface (engine/gossip_rpc.go:37-39) as provider `custom`, endpoint `fetch`, with `IsPremium` riding along for gossip's reserved bucket exactly as urchin does (app/sesame/modules/urchin.go:146). Tests reuse the `fakeGossip` canned-reply pattern (urchin_test.go:24-60).

### Deadline arithmetic

The `custom.fetch` endpoint gets a **3s** internal budget (one HTTP GET against an allow-listed URL plus cache); gossip's bus default stays 5s (engine.go:29). Sesame wraps each request at **3.5s**, slightly above the endpoint so a timeout surfaces as gossip's typed error reply instead of a client-side abort — the same reasoning that sized `gossipRPCTimeout = 12s` against gossip's 15s handler budget (gossip_rpc.go:18-24), scaled down because this endpoint has no cold-resolve tail. Overall cap equals the per-token 3.5s: fetches run in parallel, never serially. Nothing upstream bounds us today — the ctx reaching `runCustom` comes off `msg.Context()` (pipeline.go:173-174) via `bus.ConsumeWeighted` (consumer.go:70) with **no WithTimeout anywhere between delivery and expansion** — so 3.5s is self-imposed and is also why the overall figure, not a sum, is what chat latency absorbs. Subject prefix is the existing `GossipRPCPrefix` (config.go:154, default `bagel.rpc.gossip` at :234).

### Injection point — extend `tokens`, not `ParseDynamic`

Add `urls map[string]string` to the `tokens` struct (vars.go:15-25) and a `urlfetch:` branch in `expandCommand`'s switch beside the counter branch (vars.go:67-70). Justification from module/vars.go mechanics: `Expand`'s repl callback is synchronous, allocation-counted, and carries no ctx (module/vars.go:26-42), so a network hook inside expansion is impossible without faking it; `ParseDynamic` (module/vars.go:86) is the documented fallback for stateless randomness only, and routing there would re-fire per occurrence instead of once-per-name, defeating dedupe. Counters already proved this shape — pre-resolve with ctx, then a map lookup in the callback (vars.go:21-24). Unknown/unresolved names fall off the end to the verbatim-preservation path (module/vars.go:56-61): `{urlfetch:name}` stays visible like every unknown token, which doubles as the "definition missing" authoring signal.

### Failure semantics

The gossip reply carries a typed status; sesame maps it to short static text authored here, never echoing upstream bodies:

| gossip status | render |
|---|---|
| `denied` / `limited` | `[source unavailable]` |
| `upstream_error` | `[source error]` |
| `timeout` / infra error | `[source timed out]` |
| `bad_def` (missing/inactive) | token left verbatim (unknown-token convention) |

Any body gossip *does* return for interpolation passes through `ExternalVar` regardless (external_var.go:22-24): control-char strip + leading-slash trim via `sanitizeVar` (vars.go:107-109) and the 100-byte rune-safe cap (external_var.go:15,29-38). Gossip capping its own replies is not trusted — defense in depth at the variable-provider boundary is the stated contract (external_var.go:8-14). The slash-strip matters doubly here: a hostile upstream must not mint a `/ban` line through `emitResponse`'s per-line split (dispatch.go:142-148 guards user input; this closes the upstream side).

### Gate interaction

The gate runs before any fetch (runCustom: perm → live-only → cooldown claim, dispatch.go:83-100), so a failing definition consumes its cooldown window first — chat cannot hot-loop it faster than the broadcaster's cooldown. Redelivery safety mirrors `claimedCounterValue` (dispatch.go:286-326): a replay claims `EffectRef{Identity: EventIdentity(&c.Env), Effect: "urlfetch:"+name}` and skips the network call, rendering fallback text — a replayed line must not burn quota twice. Handler errors stay nil-returning (fallback text was already emitted), so nothing lands on the retry lane (consumer.go:17-19) to amplify a dead upstream.

### Observability

One segment per fan-out named `sesame.urlfetch` via `startStage`, fixed call-site string, result attribute via `endStage` — event/command/broadcaster names are attributes, never span names (telemetry.go:14-23; sibling names at pipeline.go:241,248,282,293). Gossip's side already opens `rpc bagel.rpc.gossip.custom.fetch` transactions (pkg/bus/rpc.go:119), so dashboards join on subject.

### Importer mapping

Today both importers recognize urlfetch tags only to warn: Moobot's `KNOWN_TAGS` lists `'urlfetch.plain'` and `urlfetch.json.1..10` as known-but-unmapped — left literal plus one `command_variable_unmapped` warn per distinct tag (moobot.ts:287,300,381-389); StreamElements has no `$urlfetch…` row in its variable-table decision record at all (streamelements.ts:783-794).

Design: imported urlfetch tags become named `{urlfetch:name}` references backed by synthesized definitions. **Slug rule:** deterministic `"<source>-<normalizeName(command.name)>"` — source prefix (`moobot`/`se`), command name folded through the same normalizeName the commands service write hook uses, so re-importing is idempotent and collision detection catches overlap via the existing `CollisionRef` path (types.ts:93-98). Neither export carries URLs cleanly — Moobot's `BotCommand` has no URL field (moobot.ts:246-260) and SE's `$urlfetch(url,…)` embeds the URL inline in the reply text — so SE imports extract the URL into the synthesized definition while Moobot imports synthesize a definition shell with unset URL and emit a targeted warn ("re-enter the URL for `<slug>`"). **Slots:** distinct urls in one command get index suffixes — `urlfetch.json.N` → `{urlfetch:<slug>-N}`; bare `urlfetch.plain` → `{urlfetch:<slug>}`. Two slots never merge even if their URLs look equal, because equality is unknowable until the Moobot URL is re-entered. Definition count is bounded by the existing per-import caps (types.ts:114-120).

**Goldens:** importer output is pinned by committed fixtures replayed byte-for-byte — `testdata/se-golden.json` via streamelements.test.ts:7-9,38 and `testdata/moobot-golden.json` regenerated only by the deliberate `IMPORTER_MOOBOT_DUMP_JSON` run (moobot.ts:14-17, moobot.test.ts:14-17). Rewriting tags changes golden bytes, so regenerate in the same commit as the translation change and review the diff by hand — a fixture that regenerates itself proves nothing.

## Phase 5 — console builder & rehearsal

Each piece mirrors an affordance that already ships in the console (insert-at-cursor palettes, chat rehearsal, write-only keys), so non-technical broadcasters can assemble `$(urlfetch)`-backed commands without ever seeing the wire format — while power users can. Grounded in `console/dashboard` (Svelte 5 runes: `$state`/`$derived`/`$props`/snippets, progressive-enhancement `<form>` posts via `deserialize` from `$app/forms`).

### Page & information architecture

- **New nested route `/commands/fetches`**, not an extension of the commands page. `(app)/commands/+page.svelte` is already 845 lines running the deck-list + docked-inspector pattern (`InspectorSurface`, :712-723); bolting on a second CRUD surface plus a builder would double it. The repo's nested-page precedent ((app)/settings/import/) proves the pattern: sibling folder, own `+page.server.ts`.
- Files: `routes/(app)/commands/fetches/+page.svelte` + `+page.server.ts` (actions `save`/`delete`/`testrun`), `lib/server/fetches-store.ts` (thin twin of `commands-store.ts`: `rpc<T>()` helper at commands-store.ts:22, upsert verb shape :203, delete :258), components under `lib/components/commands/fetches/`.
- Gate: copy `gateCommands` (commands/+page.server.ts:30-34) verbatim — delegates scoped to the `commands` section get fetches too, since fetch defs exist only to serve commands. Cross-link: "Fetch definitions" button in the commands toolbar trail (:667-676) and a back-link on fetches.
- Auth prologue is inherited free: hooks.server.ts runs the fleet rate limiter (:177) and `guardSession` (:187) before any action.

### Definition builder

- **Structured form first**: display name → auto-slug (`slugifyName()` added to commands-validate.ts, mirroring `normName`'s trim/lower discipline :19-21; collisions checked against the loaded list like alias de-dup at commands/+page.server.ts:108-116), URL field, response-kind radio (`plain` | `json`), optional auth = pick a stored key.
- Reuse the editor shell wholesale: `InspectorSurface` + footer `SaveStatus` states (`idle|saving|saved|error|conflict`, commands/+page.svelte:616-621) + sessionStorage drafts (drafts.ts:27-35 `bb-cmd-draft:` prefix — new `bb-fetch-draft:`).
- Save action follows `save` at commands/+page.server.ts:224-256 exactly: shared validator client-side **and** as authoritative re-check (:232-241), `tryRpc` wrapping so NATS/RpcError detail logs server-side and the client gets a generic `fail(400)` (:171-178), audit on success (:254).
- The command-response side needs zero new machinery: `{urlfetch:name}` chips join the palette in ResponseEditor.svelte (insert-at-cursor `insert()` :70-83, palette :189-196) fed from the broadcaster's saved defs.

### JSON path picker

- `FetchPathPicker.svelte` — paste-sample → tree → click-to-insert, modeled on CounterPicker.svelte (in-place create-or-pick, inserted via `onInsert` callback at ResponseEditor.svelte:194; code-chip echo style :114).
- Sample textarea capped **128 KB**, parsed with `JSON.parse` client-side only; tree rendered as a recursive snippet. Clicking a leaf builds a dotted path (`forecast.current.temp_f`, array indices as bare digits) and inserts the full token `{urlfetch:name.path}` at the caret through the same `insert()` path — never a clipboard round-trip.
- Grammar (mirrors the Go resolver): segments `[A-Za-z0-9_-]+`, depth ≤ **8**, plain-kind defs take no path. Invalid pasted JSON renders the error inline, never a thrown tree.

### Source/JSON view toggle

Segmented control flips the same underlying draft between the structured builder and a raw textarea showing the template with tokens verbatim. Unknown/malformed tokens get the `mark.unknown` treatment (ChatPreview.svelte:112,274-279) — typos stay visible, matching the engine's leave-unknown-tokens-literal rule (vars.go:49-53, proven by vars_test.go:25). Both modes call **one** shared module: `validateCommand()` extended with `validateFetchDef()` in commands-validate.ts (:73 precedent — same function powers instant client feedback and the server's authoritative re-check). Token-family familiarity already exists in the importer ($(…)/${…}/{name} scanning at streamelements.ts:760-778) — imports land as `{urlfetch:…}` candidates the page flags until a def exists.

### Rehearsal dry-run

- New `testrun` action on `fetches/+page.server.ts` — NOT the commands actions file. Client posts via the established `postAction('testrun', FormData)` + `deserialize` pattern (commands/+page.svelte:542-551). It executes the **real** path: `rpc(gossip.custom.fetch)` with `dry_run: true` (no emit), `fresh: true` (bypass the reply cache so authors see live data), the unsaved draft def inline, and dummy context equal to COMMAND_SAMPLES (user `sesame_sam`, touser `ferret_king`, args `aaaa`). Guards are the production ones by construction — same subject, same buckets, same SSRF gate as the chat path; nothing about `dry_run` weakens them.
- Reply: `{ ok, status, ms, values: { "<name.path>": string }, error? }`. The client merges `values` over the samples and renders through the existing rehearsal core + ChatPreview — line fan-out, 500-char truncation and routing preview identically to chat. Upstream errors surface in an AlertBanner (settings/import/+page.svelte:584 pattern).
- **Harder rate limit**, dedicated bucket like the import precedent (settings/import/+page.server.ts:31-44): capacity 6, refillPerSec 0.1 keyed per user — 1 run/10s sustained, burst 6 — because each run dials a third-party API; the global write tier still applies underneath. Timeout 8000 ms.

### Keys management

Copy the Govee/Spotify custody model exactly: presence read degrading gracefully on blip (govee-store.ts:211-218), `.set` sends the value **once** (:239-246), rotation = re-entering a value against the existing label. New verbs under the commands service: `fetch_set_key`/`fetch_delete`/`fetch_list` (Phase 1). List shows **label + last4 only**; the value never comes back over any verb. Input is `type="password"`, never prefilled, `autocomplete="off"`. Delete warns when referenced: the page scans loaded command responses for `{urlfetch:<label>}` occurrences client-side and lists referencing commands inside a ConfirmDialog (commands/+page.svelte:759-781 precedent); the commands service re-checks authoritatively and rejects a still-referenced delete (Go side owns the truth, console only pre-warns). Undo-toast deletion is deliberately **not** offered here — keys are destroyed, unlike snapshot-restorable command deletes (commands/+page.svelte:580-605).

### Validation parity

All numbers are the shared contract across console, commands service, and the gossip gate; the console imports them from `commands-validate.ts` so client and server literally cannot drift:

| rule | value |
|---|---|
| Def name grammar / length | `^[a-z0-9_]$` / 32 |
| Defs per broadcaster | 20 |
| URL scheme / length | https only / 512 |
| URL host | no IP-literal / localhost / `.local` / `.internal` (+ IP-logger floor) |
| Response kind | enum `plain\|json` |
| JSON path depth / segment | 8 / `[A-Za-z0-9_-]+`, indices `\d+` |
| `{urlfetch}` tokens per response | 3 |
| Response line length / count (unchanged) | 500 / 5 |
| Command name / cooldown (unchanged) | 64 / 86400 |
| Key label / value | 32 / 512 |

Tests: commands-validate additions unit-tested against this table (golden vectors shared with the Go side's table tests); testrun limiter rejects the burst-exhausting call with a 429-shaped fail; picker path-building property test (round-trip click-path ↔ resolver read on the same fixture).

## Cross-cutting

- **Config surface**: every quota is env-tunable (`pkg/env`) — def count/day budgets, bucket capacities, breaker thresholds. Defaults are placeholders pending traffic modelling; see follow-ups.
- **Observability joins**: sesame emits `sesame.urlfetch` stages; gossip opens `rpc bagel.rpc.gossip.custom.fetch` transactions with `lane=warp|direct` attributes on external segments; NR dashboards join on subject + lane.
- **Testing**: sesame uses the fakeGossip canned-reply pattern; gossip keeps core/http_test.go green through the `.Trusted()` migration (provably neutral diff); importer changes regenerate goldens in the same commit, reviewed by hand; console validators share vectors with Go table tests.
- **Security recap (defense in depth, outermost first)**: CF Gateway policy kills RFC1918 at the edge → Go SSRF gate before every dial (scheme/port/host-shape/redirect/body/content-type) → inverted `.Trusted()` default with unexported direct constructors → per-channel/per-def/per-host buckets + fleet breaker → server-side value caps → ExternalVar sanitize+cap at the variable boundary → expanded output through the existing automod floor. Keys: Tink AEAD with user+label AAD, write-only verbs, one-call plaintext, never projected/cached/logged.

## State of the world — August 24, 2026

Planning approved; **nothing implemented**. The architecture above is locked as recorded — no urlfetch entities exist in commands, gossip serves no user-defined provider, no sidecar runs beside gossip, and sesame expands no `{urlfetch:*}` tokens today. Cloudflare pricing verified Aug 2026 as cited in Phase 0.

### Known follow-ups
- Per-broadcaster quota numbers: the Phase 0 defaults are placeholders pending traffic modelling; every cap must ship configurable.
- Whether fetches respect stream-online: `Commands.stream_online_only` gates per-command today — decide whether definitions get their own gate, inherit the invoking command's, or both.
- Rehearsal rate limits for free accounts (server-side rehearsal is a real fetch and burns real upstream quota).
- WARP account/seat provisioning: which Zero Trust team, service-token issuance, Doppler secret wiring for `gossip-warp-env`.
- Decision-record comments owed for each new numeric cap (see Phase 1 Validation & quotas).

## Order / dependencies

- Phase 0 blocks everything: the token grammar and quota shape are contracts across four services.
- Phase 1 (definitions & keys) blocks Phase 2 (gossip needs something to fetch) and Phase 5 (the console edits what commands stores).
- Phase 2 (gossip provider + SSRF gate) blocks Phases 4 and 5: nothing to route, resolve, or rehearse until a fetch exists.
- Phase 3 (WARP sidecar) is deploy-only and code-independent of Phase 2, but gates public rollout: until it lands, user fetches leave from cluster IPs. Because egress choice hangs off `.Trusted()`, the flip is a deploy change, not a code change.
- Phase 4 (sesame) needs Phases 1–2; needs 3 before public launch.
- Phase 5 (console) needs Phases 1–2 only.
- Touch map: commands (ns `db`), gossip + new sidecar (ns `app`), sesame, console, `deploy/k8s`, Doppler, Cloudflare Zero Trust administration.
