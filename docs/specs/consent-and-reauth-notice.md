# Consent screen and one-time permission notice

Status: in progress on branch feature/consent-reauth. Date: 2026-11-20. Companion to [marketing-email.md](marketing-email.md).

## Goal

1. Channels whose Twitch grant lacks the scopes the dashboard now asks for get one chat line on their next stream, asking the streamer to sign in once.
2. When any owner signs in, a consent screen records the current terms acceptance (required, with version and time) and offers marketing email (optional, unticked). Existing users reach the email opt-in this way, never by email.

## User flow

1. Streamer goes live. Outgress sees a scope gap, posts one chat line, never again for that scope set.
2. Streamer opens the dashboard. An owner session with a scope gap is already redirected to `/auth/login?reauth=1` (`web/dashboard/src/routes/(app)/+layout.server.ts:71-74`). Twitch asks for the new permissions.
3. After sign-in, if the owner has not accepted the current terms version, the layout redirects to `/consent`.
4. `/consent` shows two separate boxes and one button. Done, back to the page they asked for.
5. New signups get the same component on the `/welcome` consent step, so there is one screen and one write path.

## Consent screen

- Route `/consent`, owner sessions only. Delegates and admin impersonation never see it and are never blocked by it.
- Box 1, required: "I accept the Terms of Service and Privacy Policy", with links. Continue stays disabled until it is ticked.
- Box 2, optional and unticked: marketing email. Text names ItsBagelBot, says what we send (product news and offers), gives the contact address and says it can be withdrawn any time in Settings or from any email (CASL s.10(1), Regs SOR/2012-36 s.4). Leaving it empty never blocks anything.
- Shows the address the email would go to (from `email_enc`), masked.
- Fits the viewport on desktop and phone without scrolling, fixed boxes with `data-fit` text, all six locales, no layout shift when a box is ticked.
- Reuses the existing welcome consent step markup (`web/dashboard/src/routes/welcome/+page.svelte:156-176`) and shared ui blocks. No new component library pieces.
- Form action posts both values in one request. Server writes terms first, then the email choice only when ticked.

## Data model (Users schema)

- `terms_acceptance`, append-only: user id, account incarnation (`account_created_at`), `terms_version`, accepted at, source (`welcome`, `consent`), locale, and any marketing skip reason for idempotent replies.
- Projection on `users`: `terms_version` accepted, carried on the existing dashboard state read.
- `marketing_consent` and the `users.marketing_status` projection exactly as in marketing-email.md, source `consent`.
- `email_suppression` and opt-in/withdrawal counters by source ship with the marketing-email PRs, not here.
- `terms_version` is the latest `updated` month in `web/marketing/src/content/legal/{terms,privacy}/en/meta.json`, formatted `YYYY-MM` and pinned by a test. The dashboard reads the exported constant to re-prompt owners after a policy update.
- The current `onboarded` bool stays for onboarding progress. Existing onboarded users have no recorded version, so all of them see `/consent` once.
- Erasure: both tables go with the account.

## RPCs

- `rpc.dashboard.consent_set` (owner only): published terms version, marketing choice, source, locale. One transaction, idempotent on a request id. Records the submitted version so a dashboard serving the previous version can still save consent during a rollout. Returns `marketing_skipped` (`no_email` or `unavailable`) when a requested opt-in cannot be recorded.
- `rpc.dashboard.state_get` returns `terms_version` and `marketing_status`.
- No new NATS identity, subject import or consumer: both ride the existing dashboard verb set (`app/db/users/rpc/dashboard.go:44-62`).

## Layout gate

Order in `(app)/+layout.server.ts`:

1. Scope gap: existing redirect to `/auth/login?reauth=1`.
2. Terms version below current: redirect to `/consent?next=<path>`.
3. Otherwise render.

`/consent`, `/auth/*`, `/billing` return paths, legal pages and logout are exempt so nobody gets stuck.

## One-time permission notice

### Know each grant's scopes (Users owns it)

- Twitch returns `scope` on every token refresh, but Outgress drops it (`app/twitch/outgress/internal/twitch/token.go:527-531`), and the dashboard callback keeps it only in the session (`web/dashboard/src/routes/auth/callback/+server.ts:85-88,268`). The `tokens` table has no scopes column (`app/db/users/ent/schema/tokens.go:17-36`).
- Add nullable `scopes` JSON to `tokens`. Written by `grant_save` (dashboard sign-in) and `tokens.save` (Outgress refresh). Returned by `tokens.get`. Null means unknown.

### One required-scope list

- New `internal/domain/twitchauth`: `RequiredBroadcasterScopes`, a short `SetVersion` hash, `Gap(granted)`.
- Pin tests: the dashboard `scopes()` minus `openid` (`web/dashboard/src/lib/server/oauth.ts:14-29`) must equal the Go list. `deploy/k8s/console-dashboard.yaml` omits the `DASHBOARD_LOGIN_SCOPES` override. One table test, per the policy-pin rule.

### Post once, from Outgress

- Outgress already handles `stream.online` (`app/twitch/outgress/internal/worker/streamstatus.go:177-198`), gates with `AllowsBackground` (skips unregistered, banned, dormant, disabled, blocked, bot-banned, broken grant), loads tokens and posts with `sendBotChat`.
- On go-live, after the gate passes, schedule the notice check on a bounded background worker. Load tokens with scopes. For null scopes, call `/oauth2/validate` once and compare in memory. Skip validation if the access token is missing or its stored expiry has passed. If there is a gap, recheck the gate, take a Valkey owner lock `reauthScope:<channel>:<SetVersion>` (TTL 180 days, existing `pkg_valkey.NewOwnerLock` pattern in `channels/registry.go:367-381`), then send one normal chat line. Lock before send, so it is at most once across pods and restarts.
- Signing in stores the full scope set, so the next go-live finds no gap. A future scope addition changes `SetVersion` and gives each affected channel exactly one new notice.
- Delete the uncalled `reauthBeaconOnLive` (`streamstatus.go:200-277`) instead of reviving it.
- No new NATS identity, import or consumer.

### Copy

- Key `scope.chat` in `locales/{en,fr,es,de,pt-br,ru}/chat/grant.json`, constant next to `internal/domain/i18n/i18n.go:31-32`, locale from the channel's dashboard locale (`reauth.go:148-163`).
- English draft: "Hi! ItsBagelBot needs an updated Twitch permission. The streamer can sign in once at {dashboard_url} to keep everything working."
- Normal chat, not an announcement. Nothing about email, Premium or offers.

## Pull requests

| # | Content |
|---|---|
| 1 | `internal/domain/twitchauth`, scope-list pin tests |
| 2 | Users: `tokens.scopes`, `terms_acceptance`, `marketing_consent`, projections, `consent_set`, state fields, erasure |
| 3 | Dashboard: pass scopes on sign-in, `/consent` route, layout gate, welcome reuse, six locales; privacy policy update (en, fr) for the marketing basis and Resend scope |
| 4 | Outgress: keep refresh scopes, validate fallback, one-time go-live notice, remove dead beacon, `scope.chat` copy |

Deploy order: Users (schema), then dashboard, then Outgress. Ship 4 only after 3 is live, so people who sign in find the consent screen.

## Tests (exported seams)

- Users repository on SQLite: scopes, terms and consent round trip; null scopes read as unknown; erasure removes rows.
- Users RPCs over embedded NATS: `grant_save` stores scopes, `consent_set` is idempotent and rejects delegates.
- Dashboard: layout gate order, `/consent` action writes terms and optional email, exempt routes.
- Outgress `HandleStreamEvent`: gap posts once; second go-live posts nothing; full scopes post nothing; blocked, dormant and banned channels post nothing and keep the notice; null row uses validate.
- Scope-list pins; `go test ./deploy/...`; check-i18n; screenshots of `/consent` in six locales at desktop, tablet and phone.

## Decisions for you

| # | Decision | Recommendation |
|---|---|---|
| C1 | The lifecycle spec says connection incidents send no unsolicited chat (`docs/specs/inactive-channel-lifecycle.md:141`). Allow this one-time notice as a stated exception | Yes, written into that spec |
| C2 | Where the notice runs: Outgress go-live (no contract change) or Sesame via a new projection field | Outgress |
| C3 | Remove the `DASHBOARD_LOGIN_SCOPES` override from the dashboard manifest so the code list is the only source | Done in PR 1; merging rolls the dashboard. The `dashboard-env` secret carries no such key (checked 2026-11-20) |
| C4 | Every existing owner sees `/consent` once (terms were never versioned) | Yes |
| C5 | Marketing email decisions D1 to D8 | See marketing-email.md |
