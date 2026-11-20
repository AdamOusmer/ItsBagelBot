# Marketing email: consent, unsubscribe and a shared sender

Status: PR 2 consent schema and PR 3 capture ship in `feature/consent-reauth`. The rest remains planned. Date: 2026-11-20.

## Legal answer first

Soft opt-in does not cover the 121 existing accounts.

| Region | Existing free users | Existing Premium (Tebex) | New signups after this ships |
|---|---|---|---|
| Canada (CASL) | Unclear. Only the clickwrap "written contract" route could give implied consent, untested | Weak. Tebex is the seller, not us | Express consent at signup |
| UK (PECR) | No. No opt-out was offered when the address was collected | No, same reason | Soft opt-in possible if the opt-out is shown at signup, but express is simpler |
| EU (ePrivacy) | No. CJEU C-654/23 (13 Nov 2025) lets a free account count as a sale, but the missing opt-out at collection still fails it | No, same reason | Same as UK |
| US (CAN-SPAM) | Yes, opt-out model | Yes | Yes |

Two hard rules:

- Never email existing users to ask for consent. Under CASL s.1(3) that email is itself commercial. The UK regulator fined senders for the same thing in 2017.
- Service emails (gift, giveaway, grant) stay free of any promotion.

So consent for existing users is collected in the product: a dashboard prompt and a settings row. Everyone starts ineligible.

## What exists today

- One Resend client in `app/db/transactions/mail/mail.go:57`, private `send` at `:94`, env at `app/db/transactions/main.go:218` (`RESEND_API`, `RESEND_FROM`). Callers: gift mail, admin grant mail, giveaway mail with the `award_email` ledger.
- Addresses are sealed in `users.email_enc` (`app/db/users/repository/contact_email.go:21`, AAD `<id>|contact_email`). The `email` column holds a synthetic `<id>@twitch.tv` placeholder and must never be a send target. Each login can overwrite the address from Twitch (`app/db/users/rpc/dashboard.go:163`).
- Address lookup: `rpc.internal.users.email.get` (`app/db/users/rpc/email.go:15`), imported only by Transactions.
- Onboarding stores terms acceptance as a bare `onboarded` bool (`app/db/users/ent/schema/user.go:70`), with no time or version.
- Privacy policy says the email is "never used for marketing without your consent" and that Resend sends transactional email only (`web/marketing/src/content/legal/privacy/en/01-data-collected.md:13`, `05-payment-processing.md:10`). Legal pages exist in en and fr only.

## Target design

### 1. Shared sender: `pkg/email`

Move the Resend transport out of Transactions with zero behaviour change.

- Moves: transport, `send`, validation, safe error mapping, `Receipt`, 5 s timeout, inline attachments.
- New: `Headers` on `Message` (for `List-Unsubscribe`), `FromEnv(prefix)` so `RESEND` keeps today's variable names and `RESEND_MARKETING` gets its own key and From.
- Stays in Transactions: templates, logo, `mail.json` copy, `award_email` and `giveaway_outbox` ledgers, all idempotency key formats. `mail.Mailer` keeps its public methods, so gift, grant and giveaway callers do not change.
- No Containerfile, `go.mod`, Doppler or NATS change for this step (`pkg` is already copied).
- Open and click tracking stay off.

### 2. Consent in the Users schema

Users owns consent, suppression, tokens and audience selection.

- `marketing_consent` (append-only): user id, account incarnation, status (granted, withdrawn), basis (express), source (welcome, settings, prompt, preference_page, one_click, complaint, admin), policy version, consent text key, locale, keyed HMAC of the address consented to, created at.
- Projection on `users`: `marketing_status`, `marketing_status_at` for indexed audience queries.
- `email_suppression`: address HMAC, reason (unsubscribe, complaint, hard_bounce), created at.
- A changed address (different HMAC on login) drops consent back to not granted.
- No IP or user agent stored. Time, source and text version are the CASL proof.
- RPCs: `marketing_get`, `marketing_set` (immediate write, not the 3 ms preference batch), public token `unsubscribe` and preference get/set, internal `marketing.audience` (cursor of eligible ids plus locale; addresses still resolved one at a time) and `marketing.suppress` for webhooks.
- Erasure: consent rows go with the account. Suppression keeps only the HMAC for unsubscribes and complaints, so a deleted and recreated account is not mailed by mistake. Privacy text must disclose this (`08-rights.md` promises full removal today).

### 3. Capture points

All use an unticked box, separate from the required terms box, with text that names ItsBagelBot, the purpose, a contact address and how to withdraw (CASL s.10(1) and Regs s.4).

- `/welcome` consent step, next to the terms box. Step must still fit the viewport.
- Settings, preferences section: an "Email" row (`web/dashboard/src/routes/(app)/settings/+page.svelte:593`), owner only.
- Existing accounts: one dismissible dashboard prompt (or a bell notification linking to settings). This is how the 121 accounts can opt in.
- Tebex checkout gives no consent by itself. A buyer can tick the box in our dashboard before checkout.
- Copy in all six locales.

### 4. Unsubscribe

- Token: stateless HMAC `v1.<user>.<incarnation>.<scope>.<sig>` with key id for rotation, valid at least 60 days after a send (CASL s.11), dead after erasure.
- Every marketing mail: `List-Unsubscribe: <https://.../u/<token>>`, `List-Unsubscribe-Post: List-Unsubscribe=One-Click` (RFC 8058, covered by DKIM), plus a visible footer with sender name, postal address, contact and preference link.
- One-click POST withdraws durably and returns 200 with no redirect, no cookie, no login. GET shows the preference page and never unsubscribes on its own (link scanners).
- Withdrawal is effective at once, well inside Gmail's 48 h and CASL's 10 business days.

### 5. Resend webhooks

- Verify the Svix signature on the raw body, dedupe event ids, commit before acknowledging.
- `email.bounced` (hard), `email.complained`, `email.suppressed` add a suppression row. Delivery events update the send ledger.
- Marketing withdrawals stay local: Resend suppression is team-wide and would also block receipts.

### 6. Campaigns

- Admin console: compose a versioned campaign with all six locale variants, preview eligible counts by locale, send a test to staff who opted in, approve, pause.
- Worker in Users pages eligible recipients into a send ledger keyed by campaign and address, rechecks consent and suppression right before each send, paces under Resend's 10 requests per second minus transactional headroom, stable idempotency keys, honours `Retry-After`.
- Separate marketing sending subdomain and key, with SPF, DKIM and DMARC aligned.

### 7. Analytics

Aggregates only: `accounts.marketing` by status, counters for opt-ins and withdrawals by source, campaign accepted, delivered, bounced, complained. Alert on any complaint.

## Pull request plan

| PR | Content | Ships alone? |
|---|---|---|
| 1 | `pkg/email` extraction, Transactions switched, tests ported | Yes, no behaviour change |
| 2 | Users schema (consent, projection, suppression), RPCs, erasure, analytics | Yes, nothing reads it yet |
| 3 | Privacy policy update (en, fr), consent capture on welcome, settings and the existing-user prompt, six locales | Policy must ship with or before capture |
| 4 | Unsubscribe token, one-click endpoint, preference page, header builder | Yes |
| 5 | Resend webhook endpoint and suppression sync | Yes |
| 6 | Admin campaign flow and paced worker, disabled by default | Pilot to staff first |

Ops outside git: marketing subdomain DNS records in Resend, a marketing API key in Doppler, NATS exports for the new RPCs (pinned by `deploy/messaging` tests), a route for the one-click and webhook endpoints.

## Decisions for you

| # | Decision | Recommendation | Why |
|---|---|---|---|
| D1 | Express opt-in only, or also soft opt-in for new signups | Express only | One rule for every country; soft opt-in still fails CASL clarity |
| D2 | Who sends: worker inside Users, or a new marketing service | Inside Users | No new deployment, NATS identity or secrets; consent and addresses never leave Users |
| D3 | Resend Email API with our own consent store, or Resend Broadcasts and Contacts | Email API | Consent and erasure stay in our DB; Broadcasts needs contact sync both ways |
| D4 | One-click endpoint in the dashboard (SvelteKit route calling Users RPC) or a Go listener in Users behind a new route | Dashboard route | Uses the existing public origin; no new ingress path into a data service |
| D5 | Same Resend team with a marketing key and subdomain, or a separate Resend team | Same team, separate subdomain and key | Simplest; revisit if quotas collide |
| D6 | Keep an address HMAC in suppression after account deletion | Yes, disclosed in the privacy policy | Prevents mailing someone who unsubscribed then recreated the account |
| D7 | Double opt-in confirmation email | No for in-dashboard consent | The address comes from the signed-in Twitch account; consent resets when it changes |
| D8 | Version terms acceptance too (time and version instead of a bool) | Yes, in PR 2 | Today the terms acceptance cannot be proven either |

## Not covered

- Black Friday this year: none of this can mail anyone this weekend. The in-app prompt in PR 3 is what starts building the list.
- Legal review: this plan follows the statutes and regulator guidance cited by the research lanes, not a lawyer's opinion.

## Sources

CASL s.1(3), s.6, s.10, s.11, s.13 and SOR/2012-36; ICO PECR guidance on electronic mail; CJEU C-654/23 Inteligo Media (13 Nov 2025); FTC CAN-SPAM guide; Gmail and Yahoo bulk sender requirements; RFC 8058; Resend docs on topics, broadcasts, webhooks, suppression, rate limits and pricing.
