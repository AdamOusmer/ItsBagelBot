# Premium giveaways and Tebex renewal protection

Date: 2026-09-15
Status: Product interview decisions accepted; implementation authorized after the interview. Provider scheduling verification remains a separate launch gate. See the [operations runbook](../runbooks/premium-giveaways.md).

### Confirmed decisions — review rounds 1–3 and follow-ups

- Select winners randomly from eligible registered bot accounts with completed onboarding; VIP, current staff, test, and inactive accounts are excluded. Former staff may enter when otherwise eligible.
- Subscriber prize months must match the Tebex monthly Premium offer and pass provider verification. The fulfillment correction below enables same-date nonrecurring grants, while ambiguous month-end behavior stays pending; a 30-day approximation is not used.
- Admins select 1–12 whole prize months per winner. The winner count cannot exceed the eligible pool.
- If renewal postponement cannot be confirmed, keep the award pending and raise an alert in the admin dashboard.
- Store the authoritative winner records in the Transactions database.
- Use uniform, unpredictable selection; an external library is allowed if needed.
- Send winner emails from Transactions through the existing Resend integration.
- Adapt the existing branded email template for giveaway selection and confirmation, including prize duration and current subscription state.
- Deliver prizes automatically without a claim step. Billing protection must still be confirmed before subscriber fulfillment is reported as complete.
- Previous winners may win again in later giveaways with equal odds; append their new prize duration without overlapping existing coverage.
- Notify winners when selected and explain their current subscription, including whether billing protection is confirmed or pending.
- Accounts without a usable contact email remain eligible; surface a warning instead of excluding them or losing their prize.
- Turning the bot off after winning leaves that prize and its original schedule intact. The inactive account cannot enter subsequent draws while disabled.

The sections below apply these decisions and state the proposed implementation approach and operational defaults for final review. Shared terms are recorded in [CONTEXT.md](../../CONTEXT.md).

### Fulfillment correction — September 15

After the first live draw, the user requested that fulfillment be fixed and
enabled. The global provider-verification switch had incorrectly blocked a
free winner with no recurring agreement. Nonrecurring grants now have their
own explicit enable switch and use consecutive same-date UTC monthly
anniversaries. Every monthly transition must preserve the day of month;
ambiguous month-end arithmetic stays pending rather than being clamped,
normalized into a different month, or approximated as 30 days. This enables
ordinary free-account fulfillment without claiming that Tebex billing behavior
has been verified. Subscriber protection retains the verification requirements
below. No new subscription is created for a free winner.

The original draw keeps its immutable pending interval-rule intent. On first
successful planning, Transactions saves a separate immutable fulfillment plan
containing the applied rule and absolute dates. That plan is preserved on
retries and sent to Users unchanged.

### Review round 3 outcomes

| Question | Confirmed decision |
| --- | --- |
| Q9 — Duration limit | Updated during implementation: 1–12 months per winner; the earlier uncapped decision is superseded. |
| Q10 — Onboarding | Completed onboarding is required to enter. |
| Q11 — Deactivation after selection | Keep the prize and original schedule unchanged. |
| Q12 — Former staff | Allowed when otherwise eligible. |

## 1. Outcome and recommendation

Admins can run a giveaway that awards a selected number of Tebex-equivalent months of ItsBagelBot premium to registered bot accounts. Free users and existing paying subscribers can win; VIP accounts are excluded. A paying winner receives additional premium time and has all renewals within the prize interval protected from collection, so the prize does not overlap time they have already bought.

Build this as two coordinated capabilities:

1. **Premium grants:** dated, auditable access awarded by ItsBagelBot.
2. **Renewal protection:** a verified billing change in Tebex for a winner who already has an automatically renewing subscription.

Changing a local premium expiry cannot stop a Tebex charge. Do not implement giveaways through the existing generic admin status action.

**Recommended billing mechanism:** Tebex's recurring-payment pause operation, subject to the capability checks in section 3. The preferred user experience keeps the existing subscription and resumes normal billing after the prize. If postponement cannot be confirmed, the prize remains pending and the admin dashboard raises an alert.

### Example: an existing subscriber winning three months

Assume a winner is selected on September 15 for a three-month giveaway, their paid period ends September 20 at 14:00 UTC, and the verified Tebex monthly boundaries fall on the 20th of each following month at the same time.

| Period or event | Required result |
| --- | --- |
| Before September 20 | Existing paid access remains intact. |
| September 20, October 20, and November 20 renewals | No charge for any of the three prize months. |
| September 20–December 20 | Giveaway covers premium access for the complete three-month prize. |
| December 20 | Normal billing may resume if the subscription remains enabled. |

This is the **required product behavior**, not a claim that setting a pause date automatically produces this exact schedule. Tebex's actual timing must pass section 3 before launch.

## 2. What exists in this repository

Reviewed implementation as of the date above:

| Area | Current behavior | Consequence for this feature |
| --- | --- | --- |
| [Tebex client](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/transactions/tebex/client.go:24) | Uses the Headless API to create checkout baskets. | A separate server-side subscription-management integration is needed. |
| [User billing state](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/ent/schema/user.go:67) | One source, expiry, recurring reference, cancellation flag, and billing-event cursor live on the user row. | Cannot represent independent paid access and giveaway access reliably. |
| [Admin grants](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/repository/billing.go:150) | Setting paid status records an admin source and clears the recurring reference. | A giveaway must preserve subscription identity and billing history. |
| [Billing updates](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/repository/billing.go:21) | Activations replace paid fields; matching termination events can downgrade a Tebex-sourced user. | Webhooks must update only the entitlement or subscription they concern. |
| [Expiry worker](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/repository/billing.go:184) | Expires the single stored entitlement. The worker currently gives Tebex 24 hours of grace and defaults to a five-minute sweep. | Expiry must recompute access from all valid grants. |
| [Dashboard billing](</Users/itsmavey/GolandProjects/ItsBagelBot/web/dashboard/src/routes/(app)/billing/+page.server.ts:115>) | Blocks checkout while premium is held; cancellation visibility depends on the single Tebex source. | Preserve duplicate-purchase protection and keep subscription management visible during a giveaway. |
| [Admin authorization](/Users/itsmavey/GolandProjects/ItsBagelBot/web/admin/src/lib/access.ts:55) | Moderator/admin/owner roles; premium grants require admin. | Reuse this role ladder and enforce it in the owning service. |
| [Webhook ledger](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/transactions/ent/schema/tebexwebhookevents.go:13) | Stores event and transaction identifiers, without full payment payloads. | Historical recurring references may require a Tebex payment lookup or operator reconciliation. |
| [Raffle randomness](/Users/itsmavey/GolandProjects/ItsBagelBot/app/twitch/sesame/engine/raffle_mechanics.go:45) | Uses Go `crypto/rand.Int` to select distinct winners and hashes the pool. | A secure randomness source is already available; the chat raffle's 20-winner cap and other chat policies are not giveaway requirements. |
| [Transactions email](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/transactions/mail/mail.go:15) | Sends gift email through `resend-go/v4` with idempotency keys. | Reuse the transport and styling with distinct giveaway templates. |
| [Gift email delivery](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/transactions/rpc/giftnotify.go:46) | Best-effort send; resolves contact email from Users, skips missing addresses, and logs errors without durable retry/delivery records. | Add durable award-email state in Transactions; do not copy the silent failure behavior. |
| [Admin bell](</Users/itsmavey/GolandProjects/ItsBagelBot/web/admin/src/routes/(admin)/+layout.server.ts:20>) | Shows recently sent user notifications. | Pending-award alerts require persistent unresolved state; the current bell is not an alert lifecycle. |
| [Account activity](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/repository/users.go:305) | `is_active` is the bot-enable/receive setting; it is not a last-use measurement. | The inactive-account exclusion uses this existing setting. No recent-login or streaming threshold is introduced. |
| [Staff identity](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/ent/schema/adminuser.go:14) | Users owns a separate staff roster keyed by Twitch ID; removal keeps a disabled row. | Exclude active staff membership. A historical disabled roster row does not exclude an otherwise eligible former staff member. |
| [Test-account classification](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/users/ent/schema/user.go:22) | No explicit test/system marker exists on the user. | Add an explicit Users-owned classification or ID exclusion registry with an admin management path. |

Existing code is the baseline for this proposal. No assumption is made about the live store's API permissions or payment methods.

## 3. Tebex feasibility and launch gate

### Verified public capabilities

Tebex documents `GET /api/recurring-payments/{reference}` and `PUT /api/recurring-payments/{reference}/status` on `checkout.tebex.io`. The status operation accepts `Paused` with an ISO8601 `paused_until`, or `Active` to resume. This is a pause/reactivation interface; the inspected documentation does not establish an arbitrary writable next-billing-date field. [Tebex endpoint documentation](https://docs.tebex.io/developers/checkout-api/endpoints).

The Checkout API requires prior approval. Its availability must be established separately from the bot's existing Headless integration. [Tebex Checkout API overview](https://docs.tebex.io/developers/checkout-api/overview).

The response schema exposes pause timestamps, `next_payment_date`, subscription interval, status, and cancellation information. Use actual returned values rather than the illustrative response examples. [Official Checkout OpenAPI schema](https://github.com/tebexio/TebexCheckout-OpenAPI/blob/main/checkout-api.yaml).

### Read-only access check — September 15, 2026

Following a separate request to test access, the configured Headless webstore token was retrieved from Doppler `transactions/prd` directly into process memory and used for a read-only store lookup. Tebex returned **HTTP 200** with a store object. A later public lookup confirmed the store ID matches the project ID supplied by the user. No secret values or store response data were printed or saved.

The supplied project ID was saved and read back successfully as `TEBEX_CHECKOUT_PROJECT_ID` in `transactions/prd`. Initial tests used `TEBEX_CUSTOM_SECRET`, which the user believed contained the private key. Inspection of the signed-in Tebex API Keys page through the user-authorized Arc session established that the actual current Private Key differs from that value. The current key was saved in Doppler as `TEBEX_CHECKOUT_PRIVATE_KEY` and successfully read through the CLI. The existing `TEBEX_CUSTOM_SECRET` was preserved. No key reset or regeneration was performed.

Checkout authentication requires a project ID and private key. [Tebex authentication documentation](https://docs.tebex.io/developers/checkout-api/headers-and-authentication). A read-only recurring-payment GET using a deliberately nonexistent test reference returned:

| Request | HTTP response | Sanitized provider message |
| --- | --- | --- |
| Without credentials, as a control | 403 | This action is unauthorized. |
| With the saved project ID and old custom-secret value | 401 | Unauthenticated. |
| With the saved project ID and current Private Key obtained from Tebex | 404 | No message; the test reference deliberately does not exist. |

After explicit user authorization, a read-only Headless tier lookup was tested using a synthetic user ID. The unauthenticated control returned HTTP 422 with `Basic auth credentials are required`. Tests using the saved project ID with either the old custom-secret value or the corrected Private Key returned HTTP 422 with `Invalid auth credentials provided`. For this endpoint, the Basic-auth username must be the project ID, unlike the public-token username convention used by some other Headless operations. [Tebex tier authentication documentation](https://docs.tebex.io/developers/headless-api/tiers). This endpoint remains unsuccessful and must not be treated as proof of tier-management access.

After explicit user approval to look up an existing cancelled subscription, a read-only Checkout GET returned **HTTP 200**. The response's numeric `reference` matches the requested recurring-payment reference after removing its `tbx-r-` prefix. Verification output contained only status and identifier-comparison results; no secret values or customer details were printed or saved.

**Result: the stored credential mismatch is fixed, and Checkout read access to an existing subscription is confirmed.** This establishes successful authentication and retrieval for the tested record. It does not establish authorization to pause/reactivate subscriptions, coverage of every subscription or payment method, or the resulting billing schedule. No pause, reactivation, payment, or cancellation operation was attempted. The Headless tier lookup remains unresolved separately.

### Must be resolved before automatic subscriber fulfillment is enabled

Obtain Tebex confirmation and validate in a Tebex-supported test environment:

1. **Store access:** read access is confirmed for one existing subscription; verify the store is authorized for the required pause/reactivation operations.
2. **Existing subscriptions:** verify those operations work for eligible active subscriptions originally created through the current Headless checkout. Reading a cancelled subscription does not prove this.
3. **Payment methods:** identify which payment methods actually used by the store support this operation and any restrictions.
4. **Billing semantics:** determine whether pausing preserves remaining time, changes the billing anchor, skips collections, or creates arrears. There must be no catch-up charge for any prize month.
5. **Resume semantics:** prove exactly when the first subsequent payment occurs and whether automatic reactivation produces an immediate charge. It must not precede the promised prize end.
6. **Timing limits:** determine permitted pause duration, whether an existing pause can be extended, and when a pending charge can no longer be stopped. Verify support for the entire selected X-month prize and any already-committed prize time. Protecting only its first renewal is insufficient.
7. **Cancellation:** prove that cancellation during a giveaway remains effective and is not undone by automatic reactivation or a retry.
8. **Visibility:** identify emitted webhooks and whether pause state, cancellation, and the revised schedule can be independently checked through API reads.
9. **Test evidence:** save the before/after provider state, observed payment history, and results for each supported method. A mock or synthetic webhook alone cannot prove collection was suppressed.
10. **Meaning of a month:** verify the actual monthly Premium package interval, month-end and leap-year behavior, billing time zone, and any date changes introduced by pause/reactivation. Use this same month definition for free winners. A local duration parser is not evidence of Tebex's billing contract.

The current webhook path prefers provider expiry/next-payment fields but falls back to `AddDate(0, 1, 0)` when missing; see [Transactions fallback](/Users/itsmavey/GolandProjects/ItsBagelBot/app/db/transactions/web/server.go:309). Go normalizes overflowing dates rather than clamping to the last day, so that fallback cannot establish the intended Tebex month-end policy. [Go date arithmetic](https://pkg.go.dev/time#Time.AddDate).

The adapter must translate the required billing date into the behavior Tebex actually supports. **Do not assume `paused_until = prize end` is sufficient** until these checks prove it.

If this mechanism cannot satisfy the example in section 1, automatic subscriber fulfillment remains disabled. Winner selection and the Transactions prize obligation remain separate: unsupported or unverified billing protection leaves the full award pending with an admin alert. Do not silently replace payment postponement with a coupon, local expiry change, or prize time that overlaps a paid period.

### Fallback if pause support is unavailable

Keep the winner and award durable in Transactions, leave the award pending, and raise a persistent admin-dashboard alert identifying the blocker and the affected renewal boundary. Billing difficulty is not a reason to replace the winner. Cancellation and resubscription are not the selected fallback; any exceptional support resolution would need its own agreement with the winner.

A refund is incident recovery if a charge slips through; it does not satisfy the normal promise that the user will not be charged. Do not describe refund-based fulfillment as a successful billing delay.

## 4. Product rules

Random selection, completed onboarding, current-staff/VIP/test/inactive exclusions, 1–12 whole months per winner, automatic delivery, repeat wins, pending admin alerts, Transactions ownership, and Resend winner email are confirmed. Former staff may enter; deactivation after selection preserves the already-won prize.

### Entry and eligibility

- The admin draws from registered bot account owners. Twitch chatters who have never registered are not automatically entries.
- Eligible accounts must have `is_active = true`, completed onboarding, not be banned, and exist before the pool is frozen.
- Inactive uses the existing bot-enable setting. Do not substitute `updated_at` or an inferred recent-usage threshold. Eligibility snapshots must account for accepted flag changes that have not yet flushed through Users' write-behind path.
- Free users, one-time premium users, and renewing subscribers have equal chances. Payment does not increase entry weight.
- Exclude permanent VIP accounts, current staff, test/system accounts, and inactive accounts. Use one entry per Twitch user ID and show exclusions and counts before drawing. Resolve staff/test status from authoritative identity data or explicit account flags; do not guess from usernames or email patterns.
- Use the Users staff roster for staff identity and exclude active roster membership. Former staff with a disabled roster entry may enter if otherwise eligible. Add an explicit Users-owned test/system classification or exclusion registry, editable through authorized admin actions with audit history, because that marker does not currently exist. Freeze the resolved reasons with the campaign's eligibility snapshot.
- Winner selection for this feature is by random draw. The existing generic admin grant action remains a separate capability.
- Users can win once per giveaway and may win again in later giveaways without a cooldown or reduced odds. Append the full new duration after existing continuous coverage; existing prize time is never overwritten.
- Billing difficulties do not remove an already-selected winner. Keep their award pending for resolution and raise an admin-dashboard alert.
- A usable contact email is not an entry requirement. Missing email creates a visible notification warning and does not change selection odds or prevent automatic fulfillment.
- Fulfillment is automatic with no claim deadline. A winner is not replaced for failing to open an email or visit the dashboard.
- If the winner disables the bot after the committed draw, preserve the award and its original schedule, including queued prize months. Keep the bot disabled; the award never re-enables it. The prize clock is not paused or extended, and the account is excluded from new draws until enabled again.

### Prize duration

- The admin selects a whole number from 1 to 12 months. Proposed UI default: 1 month; increments of 1. One duration applies to every winner in a giveaway.
- Subscriber prize months follow the verified interval semantics of the Tebex monthly Premium offer. Nonrecurring grants follow the bounded same-date rule described in the fulfillment correction. Calculate X consecutive monthly intervals; do not multiply a first month's day count or approximate with X times 30 days.
- The product limit is 12 months per winner, confirmed by the user during implementation. Enforce 1–12 in the form, admin server, and Transactions. Winner count is 1 through the eligible pool size and must be validated again against the frozen snapshot. Reject invalid values explicitly; never clamp or silently shorten an award. Repeated wins can accumulate beyond one campaign’s duration, so date arithmetic must still be overflow-safe and report storage representability limits.
- Copy the chosen duration into each winner record when the draw commits. Keep the draw intent immutable and save the applied interval rule in a separate immutable fulfillment plan with the first absolute dates; reuse that plan on retries. Subsequent configuration changes must not alter an existing prize.
- For an existing subscriber, use verified provider period boundaries and distinguish the paid-through date from the next collection date. Do not infer purchased coverage from a pause-adjusted next charge.
- For a free winner, the fulfillment correction above permits same-date monthly anniversaries when every transition is unambiguous; no checkout or paid subscription is created to establish the dates. Unsupported month-end behavior remains pending.
- Confirm subscriber fulfillment only after protection covers the whole requested interval. If Tebex cannot protect that duration, retain the full X-month obligation as pending and alert admins. Do not silently shorten the prize or report a partly protected interval as fully delivered.
- Record the interval rule and version used. Subscriber month-end, leap-year, time-zone, and consecutive-prize behavior must match Tebex. Nonrecurring month-end transitions that cannot preserve the date stay pending; no calendar-clamping shortcut is substituted.
- Store explicit start and end instants; access applies from the start inclusive to the end exclusive.
- Display dates in the user's locale/time zone with the time zone available. The admin detail view also shows UTC.
- Already-paid time is preserved. Future grants are appended to the end of continuous valid coverage; a disconnected future grant must not make a free user wait through an uncovered gap.

### Behavior by account state

| Winner state | Prize behavior | Billing behavior |
| --- | --- | --- |
| Free, no subscription | Start upon successful fulfillment; end after the selected X monthly intervals. | No checkout, card, or subscription required. |
| One-time purchase or admin premium, no renewing agreement | Queue after current continuous premium coverage. | No billing change. |
| Active monthly Tebex subscription | Queue X months after the already-paid period and any previously committed prize months. | Verify protection of every renewal inside the full prize interval before confirming the award. |
| Active trial | Append X months after the trial; only automate if trial-to-paid timing passes provider verification. | Delay the first payment through the full prize interval. Otherwise needs review. |
| Cancellation requested | Queue after retained paid time. | Preserve cancellation; never reactivate on the user's behalf. Confirm cancellation still prevents collection. |
| Cancellation completed | Append to remaining valid access, or start immediately if none. | Do not create another subscription. |
| Pause created by an earlier giveaway | Append the selected X months and verify the extended protection. | Do not shorten the earlier pause or protection. |
| Existing unrelated pause, overdue payment, ambiguous reference, multiple live subscriptions, or non-monthly plan | Preserve winner selection; manual review. | Do not guess at charge behavior or treat a yearly payment as a monthly prize. |
| VIP, current staff, test/system, inactive, banned, deleted, or otherwise ineligible before selection | Exclude from the draw. | Never change unrelated billing automatically. |
| Former staff with disabled roster membership | Eligible when all other entry requirements are met. | Same billing rules as any other winner. |
| Winner disables the bot after selection | Keep the prize and original schedule unchanged; do not re-enable the bot or pause the prize clock. | Preserve existing protection and complete outstanding fulfillment; do not resume billing early. |
| Other account restrictions change after selection | Record the change and apply the account-access policy separately from the recorded prize obligation. | Do not undo confirmed billing protection automatically. |

### Checkout and cancellation during a prize

- Block new self-subscription, one-time purchase, and paid-gift checkout for a recipient whose current premium/prize coverage makes the purchase redundant. Enforce the guard in the transactions service as well as the dashboard.
- Serialize checkout eligibility with award preparation where possible. Track already-issued baskets and check for late checkout completions; hiding a button cannot invalidate an existing Tebex checkout link.
- A late purchase must enter reconciliation so the winner retains the full promised prize duration. If payment occurred inside a protected interval, treat it as a billing incident.
- Subscription management stays visible whenever a relevant Tebex agreement exists, even while giveaway access is the effective source of premium.
- Cancellation affects future billing, not the remaining months of an already-awarded prize. A customer cancellation must survive restarts, webhook reordering, and retries.

## 5. Admin panel specification

Add **Giveaways** to the admin navigation, giveaway history to the user inspector, and unresolved-award alerts to the admin dashboard.

### Creation and draw

1. Create a draft with a title, internal reason, winner count, **Prize duration (months)** numeric selector. The selector accepts 1–12 whole months; default is 1. The winner count cannot exceed the eligible pool. Show a summary such as “5 winners × 3 months = 15 Premium months awarded,” using overflow-safe arithmetic. This field controls each winner's Premium duration, not how long entries stay open.
2. Preview the full eligible population and exclusions. Do not draw from the admin directory's currently loaded page or its pagination cap.
3. Freeze the candidate snapshot and campaign rules, including winner count and prize duration. Show eligible count, one-entry-per-account rule, and the split between free, premium, and subscriber accounts. Changing duration before the draw requires a new preview/freeze; after the draw it is immutable. Show any known inability to protect the selected duration in the preview. It does not change subscriber eligibility or shorten the prize: the committed subscriber award stays pending with an admin alert until full protection is verified.
4. Transactions performs the draw using server-side cryptographically secure randomness, uniformly without replacement. Every eligible account has equal probability; a draw of K winners from N candidates gives each candidate probability K/N. Persist the frozen pool, algorithm version, audit evidence, ordered result, actor, and timestamp in the Transactions database as one committed draw. Reject winner counts greater than the pool size. Stop if secure randomness fails; never fall back to timestamps, ordinary pseudorandom generators, database row ordering, or modulo-biased sampling.
5. Once committed, refreshes and retries return the same winners. There is no hidden redraw button. A draw means its eligible winners will receive the prize; admin review happens before drawing.
6. Start durable automatic fulfillment and queue selection notifications for those winner records. No winner claim or email-open event is required. The page shows progress independently for each account.

The admin approves the campaign rules and eligible pool before drawing. Winner identities become final when the draw commits; a preview must not permit repeated sampling until the admin prefers the names.

**Randomness design:** use Go's standard `crypto/rand` and unbiased integer sampling; a partial Fisher–Yates shuffle is a suitable proposed selection algorithm. No external dependency is needed for secure uniform draws. [Go cryptographic randomness](https://pkg.go.dev/crypto/rand#Int). Define a canonical candidate order before sampling and retain the committed winner order. A pool digest detects changes to the recorded pool; it does not independently prove that an operator never discarded an earlier draw. Treat draw idempotency, authorization, and audit history as separate requirements from random-number quality.

### Detail view

Show campaign status and a winner table containing:

- Twitch identity and link to the user inspector.
- Selection method and selection time.
- Selected prize duration, start/end, current paid-through date, and remaining queued months.
- Original renewal date, expected next charge date, last verified provider state/time.
- Award state: selected, preparing, needs review, scheduled, active, completed, or voided.
- Billing state: not required, pending, protected, uncertain, incident, or reconciled.
- Failure reason, retry count, and next retry time.
- Email state and a visible warning when contact email is missing or delivery fails, independently of whether Premium has already been granted.
- Actions to retry/reconcile; replace only an ineligible/declining winner with a recorded reason; export results and history.

Cancellation of a draft is allowed. After a draw, it stops only work not yet committed and requires explicit per-award handling. Never undo delivered prizes or shorten a confirmed billing pause through a generic campaign delete.

Replacement draws use the frozen pool excluding prior selections, preserve the original winner record, and record why replacement was necessary. An unavailable provider, missing email, unopened notification, or voluntary deactivation after selection is not a replacement reason.

### Pending-award alerts

A failed or uncertain renewal postponement creates an alert backed by the Transactions award and billing-operation records. Show the winner, campaign, requested number of months, target prize interval, verified protection end if any, reason, pending-since time, upcoming charge boundary, last attempt, and a link to review/retry. Preserve the alert across page reloads and process restarts. Acknowledgment must not mark the prize delivered or erase its unresolved state.

Proposed operational presentation: an unresolved-awards count and persistent summary on the admin home page, ordered by the nearest affected charge. Mark a known charge boundary inside the configured operational buffer, or a detected unexpected charge, as urgent. If timing is unknown, show that explicitly. Resolve the billing alert only when the underlying protection/fulfillment problem is durably resolved; do not clear it merely because an operator acknowledged it. Missing-email warnings stay distinct from billing alerts.

## 6. User experience

Add a prize card to dashboard billing. Transactions queues a Resend winner email when the draw commits, with the prize duration and the winner's current subscription situation. Message content reflects the latest verified fulfillment state at send time: an already-confirmed prize can be announced in that first email; a pending prize must be labeled pending. Do not wait for a claim or deliberately withhold selection notification until Tebex succeeds. If the first notice was pending, send a separate confirmation after grant commitment and verified billing protection.

Missing contact email leaves the account eligible and fulfillment automatic. Record `missing-contact`, show a warning on the admin winner row and in the unresolved notification summary, and show the prize in the user's dashboard. Email delivery failure cannot change the drawn winner or duplicate their prize. If contact later becomes available, deliver a notice reflecting the current award state rather than an obsolete pending message.

| Situation | Example copy |
| --- | --- |
| Free winner, three-month example | “You won 3 months of Premium. Your free Premium runs from September 15 to December 15. No payment is required.” |
| Subscriber, verified three-month example | “You won 3 months of Premium. You already have a recurring subscription. Your existing paid access is preserved, and your free Premium runs from September 20 to December 20. Your renewals during this period have been postponed. Your next payment is scheduled for December 20.” |
| Subscriber, pending three-month example | “You won 3 months of Premium. You already have a recurring subscription. We are arranging your free Premium period and postponing the renewals it covers. This billing change is still pending, so your current renewal schedule remains in effect until we confirm it.” |
| Subscriber, cancellation retained | “Your free Premium ends December 20. Your subscription remains cancelled and will not renew.” |

Use the stored winner duration with correct singular/plural wording and actual confirmed dates in the dashboard and Resend templates. The examples assume verified monthly boundaries on those dates. Suppress an exact “next payment” claim when Tebex only establishes a no-charge-before date; show that boundary and the pending schedule instead.

Send selection and any necessary later fulfillment confirmation through Transactions/Resend with stable per-award, per-message identities. If subscription status cannot yet be verified, say billing is being checked rather than guessing that there is no subscription. Expiry or upcoming-resumption reminders remain proposed: three days before the confirmed next charge, with immediate notice if confirmation happens inside that window. A free winner is never automatically enrolled in paid renewal.

### Reliable transactional email

- Reuse Transactions' Resend client and existing Users contact-email lookup. Never send to the legacy placeholder email, log recipient addresses, or put Tebex/Resend credentials in giveaway records.
- Use giveaway-specific HTML and plain-text templates. Existing gift copy says Premium is already active and must not be used unchanged for a pending award.
- Commit email intent alongside the relevant Transactions award transition, then send from a durable worker. Persist provider acceptance and the returned message ID; acceptance is not proof of inbox delivery.
- Resend retains idempotency keys for 24 hours. Keep a durable local send ledger, retry the same request with the same key while supported, and reconcile uncertain sends rather than blindly resending after that window. [Resend idempotency documentation](https://resend.com/docs/dashboard/emails/idempotency-keys).
- Surface unavailable contact email, delivery failures, and uncertain outcomes as warnings in the admin winner view. Missing contact must not abort automatic award processing. Do not claim that every winner was emailed merely because a best-effort send function returned.

## 7. Data and service design

Use the existing Users and Transactions services; the admin web app does not own database state or call Tebex directly.

### Ownership

| Owner | Responsibility |
| --- | --- |
| Users service | Authoritative user identity, contact-email resolution, eligibility facts, premium grants, effective tier, and idempotent grant preparation/commit. |
| Transactions service | Authoritative campaigns, frozen eligibility snapshots, draws, winner/award records, and durable fulfillment workflow; Tebex API credentials, agreement identity/history, provider snapshots, billing operations, webhook validation, reconciliation, and Resend award-email delivery. |
| Admin server | Authenticated UI and calls to the owning service using the established admin role checks. |
| Dashboard | Effective-access details, prize schedule, and separately represented subscription management. |

Winner records belong to Transactions by user decision; see [ADR 0011](../../web/docs/src/content/docs/adr/0011-transactions-owns-giveaway-winner-records.md). Keeping the campaign, draw, and winner ledger together allows one database commit to establish the draw result. Users remains authoritative for actual access. A Transactions award refers to a Users grant by stable identity; it does not require a cross-schema foreign key or direct database access.

### Required durable records

| Record | Essential fields and constraints |
| --- | --- |
| Giveaway | Transactions-owned ID, title, reason, rules/version, winner count, prize duration in whole months, lifecycle status, creator, timestamps, version. |
| Frozen candidate | Giveaway ID, stable user ID, eligibility snapshot, pool digest; unique giveaway/user pair. |
| Draw | Transactions-owned giveaway ID, operation key, pool digest, algorithm version, draw audit evidence, ordered winners, actor, timestamp; one initial committed draw per giveaway. |
| Award / winner | Transactions-owned ID, giveaway ID, stable Twitch user ID, draw ID, ordinal, immutable awarded duration in whole months, interval rule/version, planned and confirmed interval, workflow state, Users grant ID, billing-operation ID, reason/history, version; unique giveaway/user pair. |
| Premium grant | ID, user ID, source (`tebex`, `admin`, `giveaway`), source record/event/transaction ID, start/end, preparation/committed/revoked state, original anchor, timestamps. Unique source identity prevents repeat grants. |
| Tebex agreement | Store and recurring reference, user ID, product/interval, provider status, cancellation intent, next collection, paid-through value/provenance, pause state, last verification. Preserve past references; do not overwrite them when effective access changes. |
| Billing operation | Stable operation ID, award ID, agreement reference, requested protection interval, absolute target, before/after provider snapshots, state, attempts, error, lease/version, verification time. |
| Outbox and transition history | Durable work/events written with the relevant state change. State survives process exit, bus failure, or an admin closing their browser. |
| Award email | Transactions-owned award ID, message kind/version, stable delivery identity, queued/accepted/failed/uncertain/missing-contact state, provider message ID if available, attempts, and error category. Email status is independent of prize and billing status. |
| Award alert | Transactions-owned award/operation ID, category, first-seen/last-seen times, unresolved/resolved state, acknowledgment actor/time, and relevant billing boundary. Acknowledgment and resolution are separate. |

Grant dates and next collection dates are distinct. A future charge date alone is not proof of purchased access through that date, particularly during a pause or trial.

### Effective access

Keep downstream `free`, `paid`, and `vip` compatibility. VIP remains permanent. Otherwise, premium is active when at least one committed grant covers the current time; the underlying paid/giveaway provenance is exposed separately.

- Maintain account ban/activation rules independently; winning must not unban or reactivate an account.
- An expired or refunded paid grant does not revoke an unrelated giveaway grant.
- A prepared grant gives no access until committed. After a pause has been verified, recovery must commit the intended grant even if the initiating HTTP request has disappeared.
- Precompute continuous coverage and publish through the existing user-change and status-invalidation paths. Do not place a database query on every chat message.
- Recompute at boundaries and after changes. Healthy-system tier propagation target: within 60 seconds. Grant end remains the exact contractual timestamp; expose overdue processing and prevent indefinite extension on stale state.
- Retain the existing 24-hour late-renewal grace as a separate, bounded policy for confirmed renewable coverage at its expected renewal boundary. A deliberately skipped payment inside the prize interval is not a renewal failure. Grace must not consume, extend, or replace any part of the prize.

### Internal contracts

Define typed contracts for: candidate preview/freeze, draw, award lookup/retry, grant preparation/commit, subscription inspection, renewal protection, alert queries, and reconciliation. Transactions obtains eligibility facts and grant results through Users contracts; the admin calls Transactions for campaign and winner history.

Admin mutations carry verified actor identity, target/campaign ID, stable idempotency key, expected record version, and reason. Subscription references and provider targets are resolved server-side from trusted records, not accepted as arbitrary browser instructions.

The existing private billing-apply contract must evolve to identify the relevant agreement and grant. Webhooks need per-agreement ordering and durable event deduplication, rather than a single timestamp across the entire user's billing history. Payment and recurring-start/renewal events describing the same purchase must not create duplicate paid time.

## 8. Fulfillment and failure handling

### Durable workflow

1. Persist the draw, winner, durable fulfillment work item, and selection-notification intent in Transactions.
2. Recheck relevant account restrictions and acquire a durable per-account operation lease/version. Voluntary deactivation after selection does not abort fulfillment or change the award schedule. Record other post-selection changes and apply the relevant account policy; do not equate a change with permission to erase the winner. Calculate coverage without holding a database transaction open across Tebex calls.
3. Fetch current provider state for any live or unresolved agreement. Confirm store, account, package, interval, cancellation state, and upcoming collections. Treat missing or stale identity as unknown, not “no subscription.”
4. Persist the awarded month count, exact full target interval, and billing-operation intent in Transactions; prepare the corresponding Users grant through an idempotent contract keyed by award ID. These are stable across retries.
5. If billing is required, request protection through the validated Tebex adapter. Verify by read-back against the full saved interval and cancellation intent. A timeout is an unknown outcome, not permission to add the prize duration again.
6. After verified protection, idempotently commit the grant in Users. Transactions durably records the confirmed grant result and advances its own award state. Each service publishes its changes through its own outbox. For a user with no agreement, commit after the no-agreement eligibility check.
7. Transactions queues a follow-up Resend fulfillment confirmation when grant commitment and required provider verification are both durable, if the initial winner email announced a pending prize. The initial selection message is required independently of fulfillment and must explain any existing recurring subscription and the actual billing-verification state.
8. Reconcile through the original renewal boundary, the prize end, and the first subsequent settled payment or confirmed cancellation. Keep access lifecycle and billing lifecycle as separate states.

Cross-service operations are not one atomic transaction. Transactions owns recovery and follows the saved intent forward. If Tebex pause succeeded but the Users commit failed, retry the grant commit; do not immediately reactivate billing as compensation. If Users committed but its acknowledgment was lost, query or retry by the same award ID and record that existing grant rather than issuing another.

### Rules for difficult cases

- **Duplicate submissions and worker restarts:** return/resume the original operation. The target is an absolute interval, never “add X months to whatever date the retry reads.”
- **Concurrent giveaways:** serialize awards for the account and append each award's full duration once. An extended pause needs separate verification before confirming the added months. For example, a two-month prize followed by a three-month prize owes five distinct months.
- **Near renewal:** use a configurable pre-renewal buffer; 72 hours is a proposed operational default, not a Tebex guarantee. If a collection may already be in flight, keep the award pending and require reconciliation. Do not promise the imminent charge is stopped. If it settles before protection, preserve purchased time and explicitly reschedule the full still-owed prize or resolve the charge through support. This is not successful postponement of the original renewal.
- **Charge during a protected prize interval:** raise a billing incident immediately, preserve premium and the award, and initiate the agreed refund/support process. Do not conceal the charge by silently moving the prize to another month.
- **Provider timeout or outage:** bounded exponential retry with jitter; read before repeating any ambiguous mutation. Persist attempts and surface intervention before the relevant charge boundary.
- **Customer cancels:** retain the gift and cancellation. Never retry an `Active` transition merely because an award is ending.
- **Someone changes the provider pause:** reconcile against saved intent and cancellation. Do not overwrite a later unrelated pause or another administrator's change automatically.
- **Missing pause webhooks:** poll the authoritative provider state. Subscribe only to event types actually verified for this integration; do not invent required event names. Tebex's standard webhook documentation describes signed lifecycle notifications and retry behavior. [Webhook reference](https://docs.tebex.io/developers/webhooks/overview).
- **Out-of-order webhooks:** restrict updates to their agreement/transaction and reconcile ambiguous equal-time conflicts. An old termination cannot clear a new subscription or a giveaway.
- **No webhook at an intentionally skipped renewal:** retain access through the committed giveaway grant without inventing a paid renewal or revenue entry.
- **Bans/deletion after fulfillment:** follow account-access/erasure rules while retaining the minimal required award and billing-operation history. Do not restore billing as a side effect of access removal.

## 9. Permissions, audit, and operations

- Proposed minimum roles: moderator can view; admin can create, draw, award, and retry; owner handles exceptional voids and billing corrections. Server and service authorization must agree.
- Keep Tebex credentials exclusively in Transactions and restrict the new NATS subjects to their intended service callers. Admin impersonation does not grant provider mutation rights.
- Persist award/draw transitions and actor identity with the mutation. The current web audit helper is best effort, so it is a useful additional feed, not the sole giveaway ledger.
- Record grant issuance as a promotional award, not a Tebex payment. Do not count it as revenue or increment paid `gifts_sent`.
- Track selected/fulfilled/pending awards, provider verification lag, failed operations, unexpected charges, overdue tier updates, and successful resumption/cancellation.
- Reconciliation cadence: every 15 minutes for pending or protected awards; increase to every minute within two hours of a relevant boundary, subject to provider limits. Once a normal subsequent payment or terminal cancellation is verified, stop high-frequency checks.
- A feature switch may stop new awards or provider mutations. It must leave existing grants, recovery, cancellation handling, and billing monitoring operational.
- Customer-facing notices and provider mutations described here are implementation requirements only; none are sent or performed as part of writing this specification.

## 10. Migration and delivery plan

### Phase A — Prove Tebex behavior

Complete section 3, document supported payment methods and precise scheduling semantics, and decide whether the preferred mechanism is viable. This is the first milestone, before committing to subscriber automation.

### Phase B — Separate access from billing

Add grant records and agreement identity/history. Backfill current admin/Tebex access with exact stored expiries; preserve VIP. Recover missing recurring references from Tebex using stored transaction IDs where possible. The existing webhook ledger cannot reconstruct full subscription state on its own. Ambiguous accounts remain needs-review.

Make billing updates and expiry source-specific. Compare derived access with the old projection in a read-only validation period. Preserve current customer access during migration; do not manufacture an extra paid month from incomplete data.

### Phase C — Add the award workflow and UI

Implement secure random draws with persisted results in Transactions, durable grants/operations across Transactions and Users, admin views/alerts, and Transactions/Resend winner emails. Test using non-production accounts. Keep subscriber fulfillment gated until Phase A and the end-to-end checks pass.

### Phase D — Enable and observe

Start with a small controlled campaign covering free, one-time, active subscriber, and cancellation-pending cases. Observe an original renewal boundary and a post-prize billing boundary through the supported test process before general rollout. Maintain a runbook for uncertain pauses and unexpected charges.

This document records the design; the subsequent product-owner request authorized implementation, pull request, merge, and deployment. No actual giveaway draw or live customer billing mutation is part of deployment verification. Provider support messages require a separate instruction to send them.

## 11. Acceptance criteria

| Scenario | Pass condition |
| --- | --- |
| Free winner | Gets the selected number of Tebex-equivalent monthly intervals without checkout or a stored payment method; returns to free when no other coverage remains. |
| Duration selector | Defaults to 1 month, accepts whole months from 1 through 12, rejects values outside that range server-side, and previews winner count × months. Winner count cannot exceed the frozen eligible pool. |
| Unrepresentable duration/date | Returns an explicit technical validation error without truncation, wraparound, silent shortening, or creation of a malformed award. |
| Duration persistence | Committed winner records retain their original month count and dates after campaign configuration changes, retries, and service restarts. |
| Existing monthly subscriber | Existing paid time is intact, no renewal inside the full X-month prize is collected, premium covers the full prize, and normal billing resumes no earlier than the confirmed end. |
| Three-month subscriber prize | Given monthly boundaries on the 20th, a September 20–December 20 prize suppresses September, October, and November collections; December collection is the earliest permitted resumption. |
| Provider supports less than requested duration | Full prize remains owed and pending with an admin alert; a shorter pause is not reported as full fulfillment. |
| Store/gateway lacks pause support | Subscriber automation cannot report success; winner remains pending in Transactions and a persistent admin-dashboard alert identifies the blocker. |
| Current subscriber identity was cleared by an old admin grant | Account is reconciled before any “no subscription” decision. |
| Month end/leap year/DST | Interval boundaries match the verified Tebex monthly offer, including consecutive prizes; no independent 30-day or calendar-clamping shortcut is substituted. |
| Two prizes | A two-month and a three-month prize deliver five distinct months with verified protection throughout; replaying either adds no time. |
| Expiry/ended/refund webhook during a giveaway | Only related paid coverage changes; valid giveaway access survives. |
| Cancellation during pause | No resumption reverses cancellation and no subsequent renewal is collected; the prize remains valid. |
| Duplicate payment and subscription lifecycle events | No duplicate paid grant and no duplicate award/notification. |
| Pause request times out after succeeding | Read-back finds the original protection; no extra extension or premature charge is introduced. |
| Pause succeeds, local commit fails | Recovery commits the prepared grant without reopening billing. |
| Renewal races fulfillment | No false confirmation; settled payments and the still-owed prize are reconciled explicitly. |
| Browser refresh/concurrent draw calls | Same persisted winner set; no second draw or duplicate award. |
| Large eligible pool | Selection covers the complete frozen population, independent of UI pagination. |
| VIP, current staff, test, inactive, or incomplete-onboarding candidate | Excluded before selection using authoritative account data; no entry or weighting is assigned. |
| Former staff | A disabled historical staff record does not exclude an otherwise eligible account. |
| Winner disables the bot | Existing prize and dates remain intact, bot stays disabled, billing is not resumed early, and subsequent draws exclude the inactive account. |
| Automatic fulfillment | Prize processing begins after the draw without claiming, email opens, or dashboard visits. |
| Repeat winner | Remains eligible in later giveaways with equal odds; a new prize extends coverage and does not overwrite the old award. |
| Subscriber winner email | Explains the existing subscription, preserves already-paid time, and distinguishes pending protection from a confirmed postponed renewal schedule. |
| Random selection | Unweighted sampling without replacement, secure entropy, no modulo bias, one committed result, and no preview/redraw path that permits winner shopping. A statistical smoke test alone is not proof of uniformity. |
| Winner persistence | Draw and winner history survive service restarts in Transactions; Users grant retries cannot create a second award. |
| Unauthorized caller | Both admin server and service reject the action; provider credentials stay private. |
| Old checkout completes during prize | Detected and reconciled; the prize is neither erased nor silently consumed by paid coverage. |
| Missing contact email | Account remains eligible; prize fulfills automatically where billing permits, dashboard shows the prize, and admins see a persistent missing-email warning. |
| Resend delivery unavailable | Winner remains recorded; email failure is visible independently from fulfillment, and retries reuse the same message identity. |
| Process/bus outage and feature disable | Existing awards remain recoverable; protection verification and billing monitoring continue. |

**Definition of done:** an eligible paying winner demonstrably receives the full selected number of additional Premium months without paying for any part of that interval, while retaining the intended renewal/cancellation behavior. A locally extended expiry or an HTTP success response alone is insufficient evidence.

## 12. Questions to send Tebex before implementation

Suggested message, not sent:

> Our store uses the Headless API for monthly ItsBagelBot Premium subscriptions. We have verified Checkout API read access to an existing cancelled subscription. We want to award a selectable number of free monthly billing periods after a winner's current paid period, prevent collection throughout the complete prize interval, and continue the existing subscription afterward. Is our store authorized to pause these existing Headless-created subscriptions? Please confirm the monthly package's exact interval and month-end/time-zone rules, supported payment methods, maximum pause duration, how pause/reactivation affects the next collection date and remaining paid time, whether skipped periods create catch-up charges, cancellation during a pause, limits near an in-flight renewal, extension of an existing pause, available lifecycle events, and a supported way to test the complete billing cycle. For a three-month prize starting September 20 with monthly boundaries on the 20th, our required result is no September, October, or November renewal charge and the next normal charge no earlier than December 20, with cancellation still respected.
