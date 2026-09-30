# Transactions data service

Agent guide for `app/db/transactions`. Read the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md), especially Premium versus Tebex subscription.

## Responsibility and boundaries

Owns verified Tebex webhook audit records, checkout coordination and the durable
giveaway workflow in `bagel_transactions`. Tebex is merchant of record; this app
does not hold the payment ledger. Users owns billing/access application, account
eligibility and promotional Premium grants. This service reaches Users through
RPC and never opens Users' database. Notifications stores in-app notices; Resend
delivers emails through the app's mail adapter.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| Webhook event | Verified provider message identified by Tebex event ID. |
| Webhook audit status | `processed`, `failed`, `ignored`, `validation`; delivery outcome record. |
| Basket | Tebex checkout session, created by `.basket_create` RPC. |
| Gift | Paid Premium purchase for another account, distinct from a giveaway award. |
| Campaign / `Giveaway` | Admin configuration and lifecycle: draft → frozen → drawn. |
| Frozen pool | Persisted candidate membership/evidence approved before drawing. |
| Draw | Persisted secure random selection, algorithm version and pool digest. |
| Award | Durable obligation to a selected winner; selection does not mean delivery. |
| Fulfillment plan | Immutable exact award interval/rule preserved across retries. |
| Billing operation | Durable intent and evidence for provider renewal protection. |
| Agreement / recurring reference | Tebex subscription identity and inspected provider snapshot. |
| Outbox | Durable fulfillment/email work claimed by leased workers. |
| User lease | Shared serialization around one account's checkout/award workflow. |
| Alert / `needs_review` | Recorded uncertainty requiring recovery/review; award remains owed. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Boot, optional checkout/mailer, giveaway dispatcher and HTTP/TLS server. |
| [web/server.go](web/server.go) | HTTP verification/dispatch, synchronous billing apply, audit and gift callback. |
| [web/signature.go](web/signature.go), [tebexevent.go](web/tebexevent.go) | Signature check and provider event classification/parsing. |
| [repository/transactions.go](repository/transactions.go) | Immediate webhook audit insert/update; this repository is only the webhook log. |
| [rpc/checkout.go](rpc/checkout.go) | Basket creation, recipient validation and fail-closed coverage/award guard. |
| [rpc/billing.go](rpc/billing.go), [giftnotify.go](rpc/giftnotify.go) | Users billing application and best-effort gift notifications/email. |
| [rpc/giveaways.go](rpc/giveaways.go), [giveaways_campaign.go](rpc/giveaways_campaign.go) | Admin/user views, authorization, preview/freeze/draw/retry. |
| [rpc/giveaways_users.go](rpc/giveaways_users.go) | RPC adapter for Users eligibility, coverage and grants. |
| [giveaway/store.go](giveaway/store.go), [draw.go](giveaway/draw.go) | Transactional campaign/draw/outbox persistence and random/calendar primitives. |
| [giveaway/engine.go](giveaway/engine.go), [worker.go](giveaway/worker.go), [planning.go](giveaway/planning.go) | Dispatch/recovery, provider protection before grant commit and immutable interval plan. |
| [giveaway/billing.go](giveaway/billing.go), [reconcile.go](giveaway/reconcile.go) | Billing intent and provider reconciliation evidence/alerts. |
| [giveaway/engine_email.go](giveaway/engine_email.go), [mail/](mail/) | Durable award emails, prepared messages and provider delivery/templates. |
| [tebex/](tebex/) | Headless basket APIs and separate checkout/recurring provider adapters. |
| [giveaways.go](giveaways.go) | Runtime adapters and billing-event-to-award incident alerts. |
| [ent/schema/](ent/schema/) | Webhook, campaign, candidate, draw, award, plan, lease, outbox, email, agreement and alert entities. |

Contracts: [transactions](../../../internal/domain/rpc/transactions/),
[giveaways](../../../internal/domain/rpc/giveaways/),
[Users grants](../../../internal/domain/rpc/users/giveaways.go),
and [billing](../../../internal/domain/rpc/billing/). Other Ent files are generated.

## Flow and contracts

- HTTP POST `/tebex` or `/webhooks/tebex` (optional trailing slash) verifies a
  bounded body before parsing. GET aliases are inbound Tebex reachability checks.
- Accepted billing events synchronously call Users. Application failure makes
  Tebex retry; webhook audit records can update for the same event identity.
  Gift notification failures are best-effort after entitlement application.
- Default checkout prefix `bagel.rpc.transactions` exposes `.basket_create`.
  Missing webstore token/package ID disables checkout while webhook handling runs.
- Giveaway preview/freeze obtains the full authoritative Users pool. Draw persists
  selected awards and outbox intent transactionally. RPC subjects come from the
  shared giveaways contract constants, not hand-built dashboard subjects.
- Dispatcher claims durable work, plans coverage, prepares the Users grant,
  verifies/protects recurring billing when needed, then commits the grant.
  Emails distinguish winner selection from confirmed fulfillment and render
  locale-aware copy using the Users contact-email/coverage locale.

## Invariants and pitfalls

- Preserve request/draw/award identities and leases on retries. Winners are
  unique within one campaign and may win a different campaign later.
- Draw uses `crypto/rand` partial Fisher-Yates without replacement; do not fall
  back to a PRNG on failure. Winner count cannot exceed the eligible pool.
- Prize duration is 1–12 months. Provider and promotional interval rules are
  separately versioned; do not replace calendar logic with `30*24*time.Hour`.
- Saved fulfillment plans cannot be silently recalculated when coverage changes.
  Ambiguous provider state/legacy plans become review obligations.
- Gate configuration is in [giveaway/config.go](giveaway/config.go): new awards,
  promotional grants, verified provider rule and provider mutations are independent.
  Disabling new awards must leave recovery/monitoring of selected obligations alive.
- Checkout guard checks future coverage and pending/active awards under the user
  lease; unavailable/uncertain dependencies fail closed, preventing duplicate purchases.
- Users billing fields and promotional grants are separate: provider cancellation
  must not erase independently committed giveaway access or permanent VIP access.
- Attributable payment.declined renewals become payment-failed billing events;
  inspect `billingWorkFor`/`declinedRenewal` before treating all declined payments alike.
- Do not add outbound Tebex probes to health routes. TLS uses both cert/key env
  paths or neither; missing Resend credentials disable email, not in-app notices.

## Focused checks

From repository root:

```sh
go test ./app/db/transactions ./app/db/transactions/repository ./app/db/transactions/web ./app/db/transactions/rpc ./app/db/transactions/giveaway ./app/db/transactions/tebex ./app/db/transactions/mail
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/transactions/ent/schema
```

Choose webhook tests for signatures/retry, checkout tests for guarded purchase,
draw/store tests for selection idempotency, worker/engine/reconcile tests for award
recovery, and prepared/mail tests for delivery. CI regeneration/race checks:
[main.yml](../../../.github/workflows/main.yml). Tests do not establish live provider
semantics; launch gates are the explicit runtime authority.
