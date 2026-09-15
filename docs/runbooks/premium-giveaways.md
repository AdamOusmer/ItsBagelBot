# Premium giveaway operations

## Ownership and access

Transactions stores campaigns, frozen candidate snapshots, committed draws,
awards, billing operations, email progress, and unresolved alerts. Users stores
the authoritative account classification and promotional Premium grants. Each campaign awards 1–12 months per winner; winner count is bounded by the eligible pool. A
giveaway is promotional access and must never be recorded as payment revenue.

The admin console calls `bagel.rpc.admin.giveaways.*`. The ordinary dashboard
can read its prize history through `bagel.rpc.transactions.giveaways.mine`.
Only Transactions imports `bagel.rpc.internal.users.giveaway.*`. Keep these
permissions separate when extending either service.

## Provider verification

Checkout authentication has been verified by reading an existing subscription.
The monthly boundary rules, pause behavior, supported payment methods, and
absence of catch-up charges have **not** been verified. Reading a subscription
or receiving an HTTP success response is insufficient proof of protection.

Keep provider mutations and unverified interval fulfillment disabled until the
evidence required by [the specification](../specs/premium-giveaways.md#3-tebex-feasibility-and-launch-gate)
is recorded. An unresolved award remains owed in full. Its email and dashboard
must distinguish selection from confirmed delivery.

| Setting | Initial deployment | Effect |
| --- | --- | --- |
| `GIVEAWAYS_NEW_AWARDS_ENABLED` | `true` | Allows an authorized admin to create and draw campaigns. |
| `GIVEAWAYS_INTERVAL_RULE_VERIFIED` | `false` | Keeps unverified prize dates and grant fulfillment pending. |
| `TEBEX_CHECKOUT_MUTATIONS_ENABLED` | `false` | Prevents live pause/reactivation requests. |
| `GIVEAWAYS_RECONCILE_INTERVAL` | `15m` | Ordinary reconciliation cadence. |
| `GIVEAWAYS_BOUNDARY_RECONCILE_INTERVAL` | `1m` | Reconciliation near a relevant boundary. |
| `GIVEAWAYS_RENEWAL_BUFFER` | `72h` (default) | Minimum notice required before a provider pause request. |

These are explicit deployment values, independent of credential presence.
The initial release can record winners and announce an owed prize, but cannot
promise a confirmed start/end date or a postponed charge. The admin draw
preview must show this limitation before selection. Disabling new awards
must not stop recovery of existing ones.

Transactions receives credentials from Doppler `transactions/prd` through the
existing `transactions-env` secret. The Checkout adapter uses
`TEBEX_CHECKOUT_PROJECT_ID` and `TEBEX_CHECKOUT_PRIVATE_KEY`. These differ from
Headless checkout credentials. Read or validate credentials in memory; never
print secret values, Authorization headers, full provider responses, recipient
addresses, or Kubernetes Secret contents.

## Release sequence

1. Require the repository tests, web checks/builds, and the CodeScene Code
   Health Review to pass on the reviewed PR head before merging.
2. Wait for the main-branch image publishing workflow to finish. Pin the Users,
   Transactions, admin console, and dashboard images to the resulting immutable
   digests in their deployment manifests through a reviewed PR.
3. Apply only the changed NATS auth configuration to `nats-config` and
   `nats-leaf-config` in the `messaging` namespace. Preserve unrelated live
   configuration. Confirm all hub and leaf instances reload successfully.
4. Deploy Users first. Its additive tables and account marker must be available
   before Transactions starts giveaway processing. Wait for readiness.
5. Deploy Transactions, then both consoles. Wait for each rollout and inspect
   the deployed image digest and readiness counts.
6. Verify page access, role restrictions, health, and read-only history/alerts.
   A deployment smoke check must not draw real winners, send winner emails, or
   change customer billing.

Ent schema changes are additive. Retain the previous image digests for rollback
and do not drop new tables or award rows. Once an award exists, disabling new
draws must leave recovery and billing monitoring running. Reverting a service
version is not a resolution for an existing provider-side pause or owed prize.

## Handling pending awards

- Read the saved award and its alert before retrying. Retries must use the
  original award identity and absolute dates, never add months again.
- If protection is uncertain, inspect the existing provider operation before
  repeating a request. A timeout can mean the provider applied it.
- If a charge occurs within confirmed protection, preserve the award and
  Premium access, and reconcile that charge through the agreed support process.
- Keep cancellation intent intact. Do not send `Active` merely because a prize
  has ended or a grant commit failed.
- Missing email does not disqualify the winner. Track the email warning
  separately from fulfillment. Resend acceptance is not inbox delivery.
- Use the existing email layout for selection and confirmation messages. A
  pending selection message must work without confirmed start/end dates and
  must mention an existing subscription when one is known.
- Gift, selection, and confirmation emails share a flat background, separate
  content boxes, beta-feature access, and the safety footer. Confirmation
  describes automatic activation at the confirmed start; pending copy must
  never claim access or billing protection is already active.

Do not edit winner rows to change the draw, replace a difficult subscriber,
shorten a prize, or conceal a billing incident.
