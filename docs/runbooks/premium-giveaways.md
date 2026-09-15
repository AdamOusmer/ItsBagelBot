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

Keep provider mutations and subscriber fulfillment disabled until the
evidence required by [the specification](../specs/premium-giveaways.md#3-tebex-feasibility-and-launch-gate)
is recorded. An unresolved award remains owed in full. Its email and dashboard
must distinguish selection from confirmed delivery.

| Setting | Initial deployment | Effect |
| --- | --- | --- |
| `GIVEAWAYS_NEW_AWARDS_ENABLED` | `true` | Allows an authorized admin to create and draw campaigns. |
| `GIVEAWAYS_PROMOTIONAL_GRANTS_ENABLED` | `true` | Enables grants without a recurring agreement or uncertain billing; ambiguous month-end intervals remain pending. |
| `GIVEAWAYS_INTERVAL_RULE_VERIFIED` | `false` | Keeps subscriber intervals pending until provider semantics are verified. |
| `TEBEX_CHECKOUT_MUTATIONS_ENABLED` | `false` | Prevents live pause/reactivation requests. |
| `GIVEAWAYS_RECONCILE_INTERVAL` | `15m` | Ordinary reconciliation cadence. |
| `GIVEAWAYS_BOUNDARY_RECONCILE_INTERVAL` | `1m` | Reconciliation near a relevant boundary. |
| `GIVEAWAYS_RENEWAL_BUFFER` | `72h` (default) | Minimum notice required before a provider pause request. |

These are explicit deployment values, independent of credential presence.
Nonrecurring winners receive a dated grant without a Tebex request. The prize
uses consecutive same-date UTC monthly anniversaries; every requested month
must preserve its day of month. A date that would roll into another month is
kept pending for review, rather than clamped or silently normalized. The actual
rule and dates are recorded in a separate immutable Transactions fulfillment
plan and carried into the Users grant. The original draw intent is retained.
Saved plans and repeat retries never add months again.

Subscriber awards remain pending until provider protection is verified. The
admin draw preview must show this limitation before selection. Disabling new
awards must not stop recovery of existing ones.

### Verification evidence — September 15, 2026

- Rechecked three saved agreements through the live Checkout API: all reads
  returned HTTP 200 and reference comparisons matched. All three were monthly
  (`P1M`), cancelled, and their initial payments used Tebex's test method.
- None was an active recurring test subscription. Current Users records had
  no recurring reference available for a pause test.
- All three `next_payment_date` values omitted a time zone. An absolute billing
  boundary cannot be inferred from that response without a documented rule.
- A random nonexistent reference first returned authenticated GET 404. The
  status endpoint returned 403 without credentials and 404 with credentials.
  This verifies authentication/routing, **not successful pause authorization
  on a real subscription or suppression of a renewal**.
- No existing subscription was paused, reactivated, cancelled, or charged.
- Official [Checkout endpoint documentation](https://docs.tebex.io/developers/checkout-api/endpoints)
  documents the pause request but does not establish no catch-up charges,
  maximum duration, or month-end behavior. Official
  [package testing guidance](https://docs.tebex.io/creators/tebex-control-panel/how-to-create-packages/how-to-test-a-package)
  says store test mode should be used only on a private store; a fully covered
  first subscription payment does not renew automatically. The user confirmed
  there is no separate test setup.

Subscriber gates therefore remain disabled. Obtain a supported recurring test
setup and Tebex's confirmation of billing/time-zone semantics before enabling
those gates. See the provider request in the specification; endpoint access
alone must never be recorded as successful renewal protection.

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
  original award identity and absolute dates, never add months again. Use the
  admin retry operation; do not edit rows directly or redraw. Successful
  fulfillment clears the fulfillment alert while preserving unrelated email
  or billing alerts.
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
- New gift, selection, and confirmation emails include a versioned inline PNG
  logo attachment. Legacy prepared envelopes retain their original image URL
  and request body on retry. A logo cannot be replaced inside an already-sent
  message.
- Gift, selection, and confirmation emails share a flat background, clear text
  spacing, beta-feature access, and the safety footer. Only existing subscribers
  see a subscription notice box; fulfillment status is plain text. Confirmation
  describes automatic activation at the confirmed start; pending copy must
  never claim access or billing protection is already active.

Do not edit winner rows to change the draw, replace a difficult subscriber,
shorten a prize, or conceal a billing incident.
