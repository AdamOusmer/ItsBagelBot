# Gossip context

Gossip is the Go gateway for external APIs. Callers request typed data or external actions over NATS RPC; Gossip owns fetching, normalization, caching, credential resolution, and upstream budgets. It has no MySQL schema and consumes no JetStream event lanes.

Read the [shared vocabulary](../../CONTEXT.md) and [context map](../../CONTEXT-MAP.md) first. This guide describes the current source; provider availability depends on runtime configuration.

## Language

| Term | Meaning here |
| --- | --- |
| Provider | One external system, exposing a named set of RPC endpoints. This is Gossip's equivalent of a Sesame module, not a bot command. |
| Endpoint | One provider verb, addressed as `<prefix>.<provider>.<endpoint>`. The default prefix is loaded in config. |
| Flow | The declared identity → cache → admission → fetch → reply path for an endpoint. Bespoke handlers can implement different orchestration. |
| Identity | Validated request identity with separate upstream ID and cache key; account, channel, and static identities have different semantics. |
| Trusted lane | Direct HTTP egress for explicitly trusted, configuration-owned upstreams. |
| WARP lane | HTTP egress through the local WARP SOCKS proxy for untrusted destinations. This is an HTTP route, distinct from Twitch event lanes. |
| Fetch definition | A broadcaster-authored URL, extraction path, active flag, and optional key label, owned by Commands. |
| Key label | A reference to a broadcaster's stored secret, not the secret itself. |
| Dry run | A custom fetch rehearsal that can include a bounded response sample for the dashboard's field picker. |

## Code navigation

| Location | What it does |
| --- | --- |
| [main.go](main.go) | Builds infrastructure and dependencies, registers providers, wires credential/projection clients, and serves health. |
| [internal/config/config.go](internal/config/config.go) | Subject prefixes, provider credentials, enable flags, upstream URLs, cache/rate settings. |
| [internal/providers/all.go](internal/providers/all.go) | Authoritative provider registration and enable/credential gates. Start here to explain an unanswered provider subject. |
| [internal/provider/provider.go](internal/provider/provider.go) | Provider/endpoint contracts and dependency interfaces. |
| [internal/provider/builder.go](internal/provider/builder.go) | Fluent authoring API and HTTP trust declaration. |
| [internal/provider/flow.go](internal/provider/flow.go) | Cached endpoint skeleton, identity validation, reply shaping, and admission timing. |
| [internal/engine/engine.go](internal/engine/engine.go) | NATS endpoint pools, timeouts, decoding, instrumentation, and response encoding. |
| [internal/core/cache.go](internal/core/cache.go), [bytes.go](internal/core/bytes.go) | Typed and pre-marshaled caches, request collapse, freshness, negative entries, and background revalidation. |
| [internal/core/http.go](internal/core/http.go) | Shared HTTP transports, guarded destination resolution, redirect/body limits, and WARP routing. |
| [internal/core/](internal/core/) | Credential RPC clients, error/reply helpers, and limiter helpers shared by providers. |
| [shared Gossip contracts](../../internal/domain/rpc/gossip/gossip.go) | Request, reply, and subject naming shared with callers. |

Provider packages under [internal/providers/](internal/providers/) cover Urchin, Hypixel/Mojang, MCSR Ranked, Paceman, Fortnite, CODM, Govee, Clash Royale, Valorant, Spotify, and custom URL fetches. Open the named package for endpoint declarations and reply parsing; registration is centralized in `all.go`.

## Request and ownership flow

1. Sesame or a web server calls a Gossip subject. `engine.Serve` registers each endpoint in queue group `gossip-rpc`, with a separate bounded pool per subject.
2. A flow validates the identity and checks the shared Valkey cache. Cache hits can return pre-marshaled bytes directly; upstream admission is spent on actual fetches.
3. The provider calls its external API through the builder-selected transport and normalizes the result into the shared reply contract.
4. Modules supplies per-broadcaster Govee/Spotify credentials by internal RPC. Commands supplies fetch keys; projected Commands definitions supply custom fetch metadata. Gossip does not own their authoring or persistence.
5. Errors use the reply's conventional `error` field. A missing provider registration leaves its subjects unanswered; do not interpret that as an empty success response.

## Editing rules and pitfalls

- Add a provider package plus registration in `providers.All`; keep `main.go` focused on infrastructure. Add shared contracts and caller wiring when introducing new verbs.
- Declare `.Trusted()` before constructing clients. Without it, the builder selects WARP. Custom user URLs must keep the guarded WARP route; a dead sidecar fails closed rather than falling back to direct egress.
- Endpoints run concurrently with themselves. Guard captured mutable state and retain per-subject budgets so slow endpoints cannot block unrelated cache hits.
- Cache absence separately from infrastructure failure. Preserve deadline-based freshness for answers that expire at a real boundary such as a daily shop reset.
- Never put decrypted fetch keys in projections, caches, or logs. Custom fetches revalidate destinations, bound extraction, and use channel/definition/host budgets.
- Spotify credentials belong to each broadcaster's registered application and grant; do not introduce a fleet-wide Spotify application assumption.
- WARP health checks listener reachability, not successful external traffic. Its failure degrades this service because direct providers can still answer.

## Focused validation

Run from the repository root:

```sh
go test ./app/gossip/...
go test ./app/gossip/internal/provider ./app/gossip/internal/core
go test ./app/gossip/internal/providers/custom
```

For changes shared with bot variables, also test the relevant Sesame module/engine packages. For WARP routing changes, inspect [the sidecar guide](../warp/context.md) and [deployment](../../deploy/k8s/gossip.yaml).
