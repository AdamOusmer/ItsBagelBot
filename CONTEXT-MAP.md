# ItsBagelBot context map

Start here to choose the smallest relevant code area, then read that area's `context.md`. [CONTEXT.md](CONTEXT.md) remains the canonical shared product glossary; local guides add service vocabulary, responsibilities, code navigation, and focused validation commands.

These guides describe the merged source used to prepare this documentation branch. Directories containing only a context guide are explicitly marked as placeholders; unmerged local experiments are not runtime capabilities. When changing behavior, verify the named implementation and update its guide if the ownership, vocabulary, entry points, or validation commands changed. A README or design specification may describe an earlier or future state.

## Apps and owners

### Twitch processing

| App | Owns / answers | Context |
| --- | --- | --- |
| Ingress (Elixir/OTP) | Twitch EventSub connection/conduit management, admission, normalization, duplicate squashing, bus publication. | [app/twitch/ingress](app/twitch/ingress/context.md) |
| Sesame (Go) | Chat/module/command processing, incoming AutoMod, variable resolution, reply generation, timers and activity reporting. | [app/twitch/sesame](app/twitch/sesame/context.md) |
| AutoMod (Rust) | Documentation placeholder; no standalone Rust implementation on main. Incoming AutoMod lives in Sesame. | [app/twitch/automod](app/twitch/automod/context.md) |
| Outgress (Go) | Twitch API effects, chat sends, token lifecycle, authorization, and rate-limited action execution. | [app/twitch/outgress](app/twitch/outgress/context.md) |

### Discord processing

| App | Owns / answers | Context |
| --- | --- | --- |
| Ingress (Go) | The fleet's Discord gateway session, event relay, and immediate interaction defer. | [app/discord/ingress](app/discord/ingress/context.md) |
| Engine (Go) | Module decisions from Discord events and selected Twitch events; emits actions without calling Discord REST directly. | [app/discord/engine](app/discord/engine/context.md) |
| Outgress (Go) | Discord REST effects, setup/layout operations, command execution, and engine-facing RPCs. | [app/discord/outgress](app/discord/outgress/context.md) |

### Persistent data services (Go + Ent/MySQL)

Each service owns its schema. Its repository, RPC handlers, and `ent/schema` are the places to start; most other `ent` files are generated.

| App | Owns / answers | Context |
| --- | --- | --- |
| Users | Registered accounts, preferences, Twitch credentials, status/access, staff/delegation, and giveaway eligibility inputs. | [app/db/users](app/db/users/context.md) |
| Commands | Custom commands/aliases, command-use totals, and custom-fetch definitions/keys. | [app/db/commands](app/db/commands/context.md) |
| Modules | Per-channel module configuration, feature integrations/credentials, quotes, and personality/feed state. | [app/db/modules](app/db/modules/context.md) |
| Loyalty | Viewer loyalty balances, watchtime, and channel counters/entries. | [app/db/loyalty](app/db/loyalty/context.md) |
| Transactions | Billing/Tebex integration and webhook audits, giveaway orchestration, winners and award delivery. | [app/db/transactions](app/db/transactions/context.md) |
| Notifications | Account-facing notification records and their cleanup. | [app/db/notifications](app/db/notifications/context.md) |
| Discord-data | Persistent guild configuration, tickets, and XP; shared RPC-backed Discord store. | [app/db/discord](app/db/discord/context.md) |

### Shared runtime and scaffolds

| App / directory | Owns / answers | Context |
| --- | --- | --- |
| Gossip (Go) | External API providers, typed replies, HTTP egress, caches, upstream budgets, and just-in-time credential resolution. | [app/gossip](app/gossip/context.md) |
| Projector (Go) | Shared Valkey read models, owner-event folds, hydration, status/live/dashboard reads, live counters and leaderboard seeding. | [app/projector](app/projector/context.md) |
| WARP sidecar image | Vendor client image used by Gossip's untrusted HTTP route; bootstrap lives in the deployment. | [app/warp](app/warp/context.md) |
| Plain | Unfinished Go scaffold; not a wired chat engine. | [app/plain](app/plain/context.md) |
| YouTube ingress directory | Documentation placeholder with no tracked application implementation. | [app/yt-ingress](app/yt-ingress/context.md) |

### Web apps and shared packages

The [web workspace guide](web/context.md) explains app/package boundaries and workspace commands.

| App / package | Owns / answers | Context |
| --- | --- | --- |
| Dashboard (SvelteKit) | Broadcaster onboarding, bot configuration, account management, and public commands pages. | [web/dashboard](web/dashboard/context.md) |
| Admin (SvelteKit) | Staff operations, fleet/channel views, account management, trials, and promotions. | [web/admin](web/admin/context.md) |
| Marketing (Astro) | Public product site, localized marketing content, and marketing interactions. | [web/marketing](web/marketing/context.md) |
| Docs (Astro/Starlight) | Guides, variable references, and architecture decisions. | [web/docs](web/docs/context.md) |
| Kit (`@bagel/kit`) | Shared product catalogs, variable manifests, validation, server integration, and console composition. | [web/kit](web/kit/context.md) |
| UI (`@bagel/ui`) | Shared design tokens, styles, Astro/Svelte components, and interaction primitives; outside the Bun web workspace. | [ui](ui/context.md) |
| Mail templates | Static templates for human email replies. Automated transactional email is in Transactions. | [mail](mail/context.md) |

## Relationships and vocabulary traps

- **Twitch Ingress → Sesame / Projector**: normalized events use the shared lane envelope. Premium/standard are routing tiers; stream lifecycle has additional consumers. See Ingress for current dual-publication. Incoming moderation runs in Sesame; no standalone Rust consumer is present on main.
- **Sesame → Twitch Outgress**: the engine generates action messages; Outgress owns platform effects and delivery limits. Incoming AutoMod and completed outgoing reply safety are separate concerns.
- **Discord Ingress → Engine → Outgress**: receive, decide, act. Ingress's immediate interaction defer is a deliberate REST exception and shares the fleet rate budget. Run one gateway session per bot token.
- **DB owners → Projector → consumers**: owner events update shared read models, then invalidations evict process caches. A cache miss may fall back to owner RPC; no service should read another owner's MySQL schema directly.
- **Commands / Modules → Gossip**: owners retain definitions and secrets; Gossip resolves them when performing upstream requests. Projected configuration must not expose decrypted credentials.
- **Users ↔ Transactions**: Users owns account eligibility and access state; Transactions owns giveaway draws, winner records, billing, and award orchestration. A promotional Premium award is not a Tebex subscription.
- **Sesame AutoMod → Twitch Outgress**: incoming moderation decisions become described Twitch actions. Projector owns settings/live projections and has no AutoMod journal API. A future Rust extraction must establish its contracts and ownership before replacing the current Go path.
- **Web apps → Kit / UI**: Kit contains product semantics and server helpers; UI contains shared visual and interaction behavior. A variable is the product concept, a token is its literal spelling, a form is its accepted shape, and a chip is only presentation. Timers distinguish cadence, per-tick gates and per-stream stops; use the root glossary.
- **Broadcaster / chatter / bot account**: don't replace stable numeric IDs or logins with mutable display names. Account VIP, channel Twitch VIP, Premium, staff membership, trial channel, and test account are distinct.
- **Outgress** is the repository's established name for outbound action services; use the actual paths/subjects rather than searching for an `egress` app.

## Shared code navigation

| Location | Start here when changing |
| --- | --- |
| [internal/domain/event/lane/lane.go](internal/domain/event/lane/lane.go) | Elixir → Go Twitch wire envelope, sender cohorts, emote spans, trial origin, and event versions. |
| [internal/domain/event/data/](internal/domain/event/data/) | Data-owner change events and activity/loyalty contracts. |
| [internal/domain/rpc/](internal/domain/rpc/) | Cross-service Go request/reply DTOs, refusals, guards, subject conventions. |
| [internal/domain/outgress/](internal/domain/outgress/) | Twitch outbound action and job contracts. |
| [internal/domain/discord/](internal/domain/discord/) | Discord events/actions, identities, config, permissions, and validation. |
| [app/twitch/sesame/automod/](app/twitch/sesame/automod/) / [moderation engine](app/twitch/sesame/engine/moderate.go) | Current incoming-chat moderation rules, evidence, decisions and enforcement wiring. |
| [internal/projection/](internal/projection/) | Shared Valkey key layout, revision/incarnation protection, settings views, and client read caches. |
| [internal/domain/modulevars/](internal/domain/modulevars/) and [Kit variable source](web/kit/lib/variables/) | Module-template variable catalog and shared generated representation. |
| [locales/](locales/) / [internal/domain/i18n/](internal/domain/i18n/) | Canonical locale manifest and `<language>/<surface>` catalogs (`chat`, `console`, `website`, `docs`); Go bot formatting/lookup loads the chat surface. |
| [internal/moderation/](internal/moderation/) / [Sesame pipeline](app/twitch/sesame/engine/pipeline.go) | Shared banned-word matching/normalization and completed outgoing reply floor guard. |
| [internal/watchtime/](internal/watchtime/) / [internal/activity/](internal/activity/) | Viewer watchtime/activity storage and reporting infrastructure. |
| [internal/discordstore/](internal/discordstore/) | RPC-backed Discord persistence seam and ephemeral/cache state. |
| [pkg/bus/streams.go](pkg/bus/streams.go) / [pkg/bus/](pkg/bus/) | JetStream catalog, provisioning, publication, consumption, retry/failback, RPC pools and transport conventions. |
| [pkg/svcboot/](pkg/svcboot/) / [pkg/svcboot/databoot/](pkg/svcboot/databoot/) | Shared Go lifecycle and database-service boot. |
| [pkg/db/](pkg/db/) / [pkg/valkey/](pkg/valkey/) | Infrastructure connectors, pool/health behavior, and primary/replica routing. |
| [pkg/tmpl/](pkg/tmpl/) / [pkg/tzname/](pkg/tzname/) | Template expansion and Local Time place/timezone lookup. |
| [deploy/k8s/](deploy/k8s/) / [deploy/messaging/](deploy/messaging/) | Deployment wiring, service ACLs, subjects, credentials, and NATS topology. |
| [.github/workflows/](.github/workflows/) | Actual CI commands and generation steps. |
| [web/docs/src/content/docs/adr/](web/docs/src/content/docs/adr/) | Recorded architectural decisions; check current code before treating a proposal as implemented. |

## Efficient change workflow

1. Select the owning app from this map and read its guide plus the shared glossary. Open the few relevant files listed there before broad searches.
2. Follow a cross-service change through owner → contract → transport/projection → consumer. Keep ownership, authentication, invalidation, and retry behavior intact.
3. Edit handwritten Ent schemas rather than generated clients. If generation is needed, retain both `sql/upsert` and `sql/lock`; the Go CI workflow documents the invocation.
4. Run the guide's focused checks. Go commands run from the repository root; Bun, Mix, and Cargo commands state their required directories in local guides. Documentation-only edits need link/coverage validation rather than full service startup.
5. Update the relevant context when its facts change. Keep guides selective: no dependency dumps, generated-file inventories, secrets, transient deployment measurements, or duplicated shared glossaries.

Source/runtime build artifacts under `node_modules`, `target`, `_build`, `deps`, `.svelte-kit`, `.astro`, `build`, and `dist` are not app boundaries. `output`, `brag-output`, and `ad_production` contain generated/design/media work rather than production application owners in this map.
