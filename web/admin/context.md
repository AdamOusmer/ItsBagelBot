# Admin console context

Use [shared terminology](../../CONTEXT.md) and the [context map](../../CONTEXT-MAP.md)
first. This guide describes the operator-facing SvelteKit app, not broadcaster settings.

## Responsibility and boundaries

- `admin` is a Bun workspace package; production runs through SvelteKit adapter-node.
- Staff inspect accounts, operate ingress, manage deploy runs, send notifications and manage access.
- Users, transactions, ingress, and other backing services own persistent business state.
- Route actions validate/operator-authorize requests and call those services over NATS.
- The tailnet is a network boundary; active staff membership is a separate identity gate.
- Generic visual primitives belong to [ui](../../ui/context.md); bot-aware shells/server helpers
  belong to [kit](../kit/context.md).

## Nomenclature

| Term | Meaning here |
| --- | --- |
| Operator / admin identity | Signed-in Twitch identity currently authorized as staff. |
| Staff role | `moderator`, `admin`, or `owner`; ordered by the shared staff rank. |
| Access key | Named capability such as `users.grant` or `secrets.manage`. |
| View as / impersonation | Short-lived admin session for inspecting a broadcaster dashboard. |
| Shard | Twitch EventSub Conduit delivery slot managed by a shard session; separate from an AutoMod partition or pod. |
| Lane | Operator view of a JetStream stream/consumer pipeline; includes alias/durability controls. |
| Trial channel | Unregistered Twitch channel observed without acting on its behalf. |
| Giveaway campaign | Promotion whose eligibility pool is previewed/frozen before drawing winners. |
| Award | Winner delivery record; selection, billing protection, and email delivery have distinct states. |
| Deploy run | Service-owned release/deployment operation with plan, stages, approval and live events. |
| Neutral fallback | Empty/zero display after unavailable data, never invented live success. |

Root terminology governs Premium, prize months, VIP, current/former staff, and active accounts.
Twitch moderation privileges and ItsBagelBot staff roles are separate concepts.

## Start here for changes

- [hooks.server.ts](src/hooks.server.ts): one-time runtime boot, session decoding,
  staff revocation gate, public-route exemptions, locale, headers and telemetry.
- [access.ts](src/lib/access.ts): single `ROLE_FOR` table plus `allows`, `canManage`,
  and `grantableRoles`; safe for browser imports.
- [server/access.ts](src/lib/server/access.ts): `requireAdmin` rechecks active staff;
  `requireRole` enforces an access key and fails closed on auth-service errors.
- [services.ts](src/lib/server/services.ts): RPC subjects, wire mappings, reads/writes,
  cache fabric and scope invalidation; default admin L1 capacity is 250 entries.
- [admin-action.ts](src/lib/server/admin-action.ts): common parse/refusal and audited
  mutation helpers; [audit.ts](src/lib/server/audit.ts) records operator activity.
- [giveaways.ts](src/lib/server/giveaways.ts): campaign/award wire states and adapters;
  freeze/draw operations carry expected versions and idempotency keys.
- [lanes.ts](src/lib/server/lanes.ts) and [lane-telemetry.ts](src/lib/server/lane-telemetry.ts):
  JetStream administration, store HA reconciliation, delivery rates, and pagination.
- [deploys.ts](src/lib/server/deploys.ts): owner-gated planning/start/resume/cancel/approval;
  streamed plans and run events are backed by deploy RPC adapters in services.
- [secrets.ts](src/lib/server/secrets.ts): service-specific credentials and rotation;
  use its service registry and scoped tokens instead of general secret reach-ins.

## Route areas and request flow

- `src/routes/(admin)/+layout.server.ts` gates the group and streams recent notification data.
- `(admin)/+page.*` shows overview; `shards/` handles Conduit operation, `analytics/` telemetry,
  and `lanes/` pipeline operations.
- `users/` inspects accounts, bans, grants, test flags, restarts and view-as links.
- `giveaways/` and `giveaways/[id]/` manage promotion creation, freeze, draw and retries.
- `trials/`, `counters/`, `notifications/`, `audit/`, `staff/`, and `secrets/` own their named areas.
- `data/`, `snapshot/`, `history/`, and similar `+server.ts` routes serve live/detail reads.
- `auth/login`, `auth/callback`, `auth/logout`, and `login` handle operator sign-in.
- Exact `auth/bot/login`, `callback`, `done` routes are public according to
  [public-routes.ts](src/lib/server/public-routes.ts), and create no staff session. Callback verifies
  configured `TWITCH_BOT_USER_ID`, OAuth state/nonce, audience/issuer and required bot scopes before save.
- `deploys/`, `deploys/new/` and `deploys/[id]/` manage owner-only deployment runs and SSE detail.
- `healthz` and `readyz` are public probes; protected health views are separate.
- Browser form → route action → `requireRole` → RPC adapter → owning service → cache invalidation.
- Staff invalidation evicts `auth:` cache entries across replicas; layouts alone do not guard actions.

## Invariants and common mistakes

- Button visibility is courtesy; repeat authorization in actions and `+server.ts` handlers.
- `DEMO` must be gated directly by build-time `dev`; production checks remove fixture routes/imports.
- Boot import graphs read `process.env`; `$env/dynamic/private` there can deadlock `server.init()`.
- Subject defaults use `||`: a present-but-empty variable must not become a leading-dot RPC subject.
- Keep mutations audited and distinguish backend refusal from unavailable reads.
- Do not interpret a selected giveaway winner as a fulfilled Premium award.
- Public bot consent is identity-bound; do not weaken configured-ID/state/claim/scope checks.
- Shared cache policies live in kit; do not add a private auth cache with independent TTLs.

## Focused commands

Run from `web/admin`: `bun run dev`, `DEMO=1 bun run dev`, `bun run check`, `bun run build`.
Build runs demo gating, shared i18n/size checks, Vite build, and production-clean verification.
Run from `web`: `bun test admin/test` for the operator helper/contract tests.
For a narrow change: `bun test admin/test/giveaways.test.ts` or `admin/test/lane-telemetry.test.ts`.
Real integration behavior needs matching runtime configuration and backing services;
demo mode establishes UI behavior, not production authorization or delivery correctness.
