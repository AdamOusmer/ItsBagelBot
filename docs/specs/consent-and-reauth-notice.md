# Consent screen and one-time permission notice

Status: implemented on branch `feature/consent-reauth`. Date: 2026-11-20. Companion to [marketing-email.md](marketing-email.md).

## Goal

1. Channels whose Twitch grant lacks the scopes the dashboard now asks for get one chat line on their next stream, asking the streamer to sign in once.
2. When any owner signs in, a consent screen records the current terms acceptance (required, with version and time) and offers marketing email (optional, unticked). Existing users reach the email opt-in this way, never by email.

## User flow

1. The streamer goes live. Outgress checks for a scope gap and claims the one-time chat notice for that scope set before sending it.
2. The streamer opens the dashboard. An owner session with a known scope gap redirects to `/auth/login?reauth=1`. Twitch asks for the new permissions.
3. After sign-in, existing owners whose known terms version is below the current version are redirected to `/consent` on gated pages.
4. `/consent` shows two separate boxes and one button. After saving, the owner returns to the requested page.
5. New owner signups redirect to `/welcome`. Its consent step shares the form and write path with `/consent`.

## Consent screen

- Route `/consent`, owner sessions only. Delegates and admin impersonation never see it and are never blocked by it.
- Box 1, required: "I accept the Terms of Service and Privacy Policy", with links. Continue stays disabled until it is ticked.
- Box 2, optional and unticked: marketing email. Text names ItsBagelBot, says what we send (product news and offers), gives the contact address and says it can be withdrawn any time in Settings or from any email (CASL s.10(1), Regs SOR/2012-36 s.4). Leaving it empty never blocks anything.
- Shows the address the email would go to (from `email_enc`), masked.
- Fits the viewport on desktop and phone without scrolling, fixed boxes with `data-fit` text, all six locales, no layout shift when a box is ticked.
- Both routes use [ConsentForm.svelte](../../web/dashboard/src/lib/components/ConsentForm.svelte) and shared UI blocks.
- Form action posts both values in one request. Server writes terms first, then the email choice only when ticked.

## Data model (Users schema)

- `terms_acceptance`, append-only: user id, account incarnation (`account_created_at`), `terms_version`, accepted at, source (`welcome`, `consent`), locale, and any marketing skip reason for idempotent replies.
- The `users.terms_version` projection carries the highest accepted version on the dashboard state read. The ledger always records the submitted version.
- `marketing_consent` records express consent and the address HMAC. Changes to the contact address withdraw any grant. If the HMAC key is unavailable and the existing grant cannot be verified, saving the address also withdraws the grant.
- `email_suppression` and opt-in/withdrawal counters by source ship with the marketing-email PRs, not here.
- `terms_version` is the latest `updated` month in `web/marketing/src/content/legal/{terms,privacy}/en/meta.json`, formatted `YYYY-MM` and pinned by a test. The dashboard reads the exported constant to re-prompt owners after a policy update.
- The `onboarded` flag tracks onboarding progress. Users returns an empty version for an account with no acceptance, which triggers the terms gate on a gated page.
- Erasure: both tables go with the account.

## RPCs

- `rpc.dashboard.consent_set` (owner only): published terms version, marketing choice, source, locale. One transaction, idempotent on a request id. Records the submitted version so a dashboard serving the previous version can still save consent during a rollout. Returns `marketing_skipped` (`no_email` or `unavailable`) when a requested opt-in cannot be recorded.
- `rpc.dashboard.marketing_set` (owner only) records an opt-in or withdrawal from Settings. Both consent writes carry the actor, delegation and impersonation fields from the authenticated session.
- `rpc.dashboard.state_get` returns `terms_version` and `marketing_status`.
- These RPCs use the existing [dashboard verb set](../../app/db/users/rpc/dashboard.go).

## Layout gate

The [app layout](../../web/dashboard/src/routes/%28app%29/+layout.server.ts) checks in this order.

1. Scope gap: existing redirect to `/auth/login?reauth=1`.
2. Known terms version below current: redirect to `/consent?next=<path>`.
3. Otherwise render.

An older Users pod can omit `terms_version` during rollout. The layout treats the version as unknown and skips the terms redirect. The welcome flow still requires terms acceptance.

`/settings` is exempt from the terms gate so owners can manage their email preference. It still checks Twitch scopes. `/consent`, `/auth/*`, billing checkout returns, legal pages and logout bypass the layout gates.

## One-time permission notice

### Know each grant's scopes (Users owns it)

- The [tokens schema](../../app/db/users/ent/schema/tokens.go) stores nullable `scopes` JSON. The dashboard callback writes the Twitch grant through `grant_save`. Outgress writes refresh scopes through `tokens.save` and reads them through `tokens.get`.
- Null means unknown. If Twitch returns an empty scope list at sign-in, `grant_save` sends null and Outgress checks the grant through validation.

### One required-scope list

- [The Go scope list](../../internal/domain/twitchauth/scopes.go) supplies `SetVersion` and `Gap(granted)`.
- Pin tests keep the Go list aligned with [dashboard OAuth scopes](../../web/dashboard/src/lib/server/oauth.ts), excluding `openid`. The dashboard deployment manifest omits the `DASHBOARD_LOGIN_SCOPES` override.

### Post once, from Outgress

- [HandleStreamEvent](../../app/twitch/outgress/internal/worker/streamstatus.go) schedules [the scope notice](../../app/twitch/outgress/internal/worker/scope_notice.go) on a bounded background worker once the channel passes background admission.
- For null scopes, Outgress calls `/oauth2/validate` once per check and compares in memory. It skips validation if the access token is missing or its stored expiry has passed. If there is a gap, Outgress rechecks admission and claims the Valkey owner lock `reauthScope:<channel>:<SetVersion>` for 180 days before sending a normal chat line. The claim gives at-most-once delivery across pods and restarts for its lifetime. A send that fails after the claim can lose the notice because the claim is retained until it expires.
- Signing in with all required scopes clears the gap. A future scope addition changes `SetVersion` and makes affected channels eligible for a new notice.
- The legacy `reauthBeaconOnLive` was removed.
- No new NATS identity, import or consumer.

### Copy

- Key `scope.chat` in `locales/{en,fr,es,de,pt-br,ru}/chat/grant.json`, referenced by `KeyGrantScopeChat`. [ResolveLocale](../../app/twitch/outgress/internal/worker/reauth.go) uses the channel's dashboard locale.
- English: "Hi! ItsBagelBot needs an updated Twitch permission. The streamer can sign in once at {dashboard_url} to keep everything working."
- Normal chat, not an announcement. Nothing about email, Premium or offers.

## Release

The branch contains the complete implementation. Slice the changes into these PRs at release.

| # | Content |
|---|---|
| 1 | `internal/domain/twitchauth`, scope-list pin tests |
| 2 | Users: `tokens.scopes`, `terms_acceptance`, `marketing_consent`, projections, consent RPCs, state fields, erasure |
| 3 | Dashboard: pass scopes on sign-in, `/consent` route, layout gate, welcome reuse, six locales; privacy policy update (en, fr) for the marketing basis and Resend scope |
| 4 | Outgress: keep refresh scopes, validate fallback, one-time go-live notice, remove dead beacon, `scope.chat` copy |

Set the Doppler prerequisites before release.

- `dashboard-env` needs `BUSINESS_MAILING_ADDRESS` for marketing opt-in. An empty value logs a startup warning and disables opt-in.
- `users-env` needs `MARKETING_HMAC_KEYSET` for the address HMAC.

Deploy Users with the schema and RPCs first, then the dashboard, then Outgress. The consent screen must be live before Outgress starts posting sign-in notices.

## Tests (exported seams)

- Users repository on SQLite: scopes, terms and consent round trip; null scopes read as unknown; erasure removes rows.
- Users RPCs over embedded NATS: `grant_save` stores scopes, `consent_set` is idempotent and rejects delegates.
- Dashboard: layout gate order, `/consent` action writes terms and optional email, exempt routes.
- Outgress `HandleStreamEvent`: gap posts once; second go-live posts nothing; full scopes post nothing; blocked, dormant and banned channels post nothing and keep the notice; null row uses validate.
- Scope-list pins; `go test ./deploy/...`; check-i18n; screenshots of `/consent` in six locales at desktop, tablet and phone.

## Decisions for you

| # | Decision | Recommendation |
|---|---|---|
| C1 | Allow the one-time permission notice as an exception to the connection-incident chat rule | Yes, documented in [section 4 of the lifecycle spec](inactive-channel-lifecycle.md#4-dormancy-and-automatic-wake-up) |
| C2 | Where the notice runs: Outgress go-live (no contract change) or Sesame via a new projection field | Outgress |
| C3 | Remove the `DASHBOARD_LOGIN_SCOPES` override from the dashboard manifest so the code list is the only source | Removed in release slice 1 |
| C4 | Every existing owner sees `/consent` once (terms were never versioned) | Yes |
| C5 | Marketing email decisions D1 to D8 | See marketing-email.md |
