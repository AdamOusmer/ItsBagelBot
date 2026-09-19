# Local Time module: place lookup

Date: 2026-09-18
Status: Implementation in progress

## 1. Outcome

The Local Time module (`time`) keeps answering a bare `!time` with **home time**: the current time in the broadcaster's configured **home zone**. It also answers `!time <place>` (**place lookup**): a viewer names a **place** (a city, a timezone name, an abbreviation, or a UTC offset) and the bot answers with the current time there, in the channel's clock format. An unrecognized place gets a hint reply, never a silent fallback to home time.

## 2. Decisions

| # | Decision | Rationale |
| --- | --- | --- |
| D1 | Vocabulary: **home zone** (the broadcaster's configured IANA zone), **home time** (the answer to a bare `!time`), **place** (whatever a viewer typed after `!time`), **place lookup** (the answer to `!time <place>`). | One name per concept keeps the code comments, the spec, and [CONTEXT.md](../../CONTEXT.md) saying the same thing. |
| D2 | Accepted place shapes: city names (IANA zone names, plus a curated alias list of roughly 70 big cities IANA's own names miss, such as Montreal, Seattle, Beijing, Mumbai), IANA names in any case, abbreviations (EST, PT, JST, …), and UTC offsets (`UTC+2`, `+05:30`, `-7`). Not supported: country names, US states. | A multi-zone country or state (`!time USA`, `!time California`) would force the bot to guess a city; better to ask for one. |
| D3 | Ambiguous abbreviations get one declared meaning: `CST` → Chicago, `IST` → Kolkata, `BST` → London. Recorded as a code comment next to the curated table. | A wrong pick costs the viewer one retype with a city name; a tri-state "which did you mean" round trip costs more than it's worth for a chat bot. |
| D4 | An unrecognized place replies with a hint: `I don't know where "X" is. Try a city (Tokyo), a zone (Europe/Paris) or an offset (UTC+2).` Never falls back to home time. The command cooldown is consumed either way. | Silently answering with the streamer's own time for an unrecognized place would look like a wrong answer rather than a miss; the hint tells the viewer what shape to retry with. |
| D5 | Lookup works even when the home zone is unset. A bare `!time` with no home zone still answers that the zone is unset. | The two replies are independent: a lookup needs only the place the viewer named, not the channel's own configuration. |
| D6 | Second reply template `lookupMessage`, default `It is currently {time} in {place}.`, variables `{time} {date} {place} {timezone} {user}`. `{timezone}` is the resolved IANA name, or the offset as typed (`UTC+2`) when the place is an offset. `{place}` is the display label the bot names it by (`Toronto`, `Eastern Time`, `UTC+2`). The home template (`message`, `{time} {date} {timezone} {user}`) is unchanged. | Keeping `{place}` distinct from `{timezone}` lets a broadcaster write a reply that reads naturally ("It is {time} in {place}") without spelling out the raw zone name. |
| D7 | No broadcaster opt-out toggle for lookup in v1. | Nothing about a curious viewer asking for another city's time needs gating; a toggle can follow if broadcasters ask for one. |
| D8 | Out of scope for this change: a `{time:<place>}` payload for custom-command variables, a Discord `!time`, and importer mapping of Nightbot/Fossabot `$(time <tz>)`. | Each is a separate surface with its own resolver wiring; bundling them here would block this feature on unrelated decisions. |
| D9 | Coverage source: the IANA zone list generated from the Go toolchain's zoneinfo.zip ([pkg/tzname/zones_gen.go](../../pkg/tzname/zones_gen.go), golden-tested), plus the curated alias table. No external dataset, no geocoding API. `Etc/*` zones are excluded (their sign is inverted from common use, e.g. `Etc/GMT+5` is five hours *behind* UTC). Link names (`Asia/Calcutta`) are accepted only as full names, never abbreviated further; the segment index (city-name lookup) covers canonical zone names only. Collisions between two canonical zones sharing a city segment (`Asia/Istanbul` vs `Europe/Istanbul`) are resolved by curated entries, and a test asserts zero unresolved collisions. | A dataset or geocoding dependency would need its own update cadence and failure mode; the Go toolchain's own zoneinfo is already vendored and versioned with the build. |
| D10 | The clock face for a lookup follows the broadcaster's 12/24 `format` setting, same as home time. Both modes share the same 15 second command cooldown. | One clock-format setting per channel, not two; a viewer asking about Tokyo should see the same hour notation the channel already uses. |
| D11 | Argument normalization: trim, collapse internal whitespace, cap at 64 runes, strip a leading `@` or `#`, strip trailing `?`, `!`, `.`, accent-fold (`São Paulo`, `Montréal`), case-fold, treat `_` as a space. The whole remainder (after normalization) is the place; multi-word places are fine. | Chat input is messy (mentions, punctuation, casing); normalizing once at the front means every matcher downstream sees a clean string. |
| D12 | Offset grammar: optional `UTC`/`GMT` prefix, a required sign, hours `0`-`14`, minutes restricted to `{00, 15, 30, 45}`. Bare digits with no sign (`!time 5`) are rejected, not interpreted as an offset. | A sign-less number is ambiguous with a city or abbreviation typo; requiring the sign keeps offset parsing unambiguous. |
| D13 | The three fixed replies (`time.unset`, `time.unavailable`, `time.unknown`) are localized through the bot's `i18n.T`, in English and French. This also fixes pre-existing hardcoded English strings in `timeofday.go`. | Every other sesame reply already routes through `i18n.T`; the Local Time module's fixed lines were the exception. |
| D14 | New package [pkg/tzname](../../pkg/tzname) owns `Resolve(query) (Match, bool)` and the `time/tzdata` import. | One package owns the zone data and matching logic; `timeofday.go` and `time_config.go` only call `Resolve`, they don't parse zone data themselves. |
| D15 | This spec lives at `docs/specs/time-place-lookup.md`; the glossary terms land in [CONTEXT.md](../../CONTEXT.md). No ADR: the abbreviation picks in D3 are reversible and recorded in the code comment, not an irreversible architectural choice. | Matches this repo's ADR bar: an ADR records a decision that is expensive to reverse later. |

## 3. Behaviour

| Input | Home zone | Result |
| --- | --- | --- |
| `!time` | set | Home time reply (`message` template). |
| `!time` | unset | `time.unset` hint; unchanged from today. |
| `!time Toronto` | any | Lookup reply (`lookupMessage`), `{place}` = "Toronto". |
| `!time America/New_York` | any | Lookup reply, full IANA name accepted any case. |
| `!time europe/paris` | any | Lookup reply, lower-case IANA name accepted. |
| `!time EST` | any | Lookup reply, abbreviation resolved per the curated table (D3). |
| `!time UTC+2` | any | Lookup reply, `{timezone}` = "UTC+2". |
| `!time Narnia` | any | `time.unknown` hint (D4); cooldown still consumed. |
| `!time Tokyo` | unset | Lookup reply answers normally (D5); independent of home zone. |

## 4. Resolution order

`pkg/tzname.Resolve(query)` tries, in order, until one matches:

1. **Offset grammar** (D12): `UTC`/`GMT` prefix, sign, hours, quarter-hour minutes.
2. **Full IANA name**, case-insensitive (`America/New_York`, `europe/paris`).
3. **Curated alias** (city name or abbreviation not in IANA's own naming, or an abbreviation with a declared meaning per D3).
4. **City-segment index** over canonical IANA zone names (the last path segment, e.g. `Paris` from `Europe/Paris`), built from the generated zone list (D9), collisions resolved by the curated table.

No match at any step is an unknown place (D4).

## 5. Reply templates

| Reply | Config key | Default | Variables |
| --- | --- | --- | --- |
| Home time | `message` | `It is currently {time} for the streamer.` | `{time} {date} {timezone} {user}` |
| Place lookup | `lookupMessage` | `It is currently {time} in {place}.` | `{time} {date} {place} {timezone} {user}` |

`{timezone}` is the resolved IANA name, or the offset as typed for an offset place. `{place}` is the display label (D6).

## 6. Dashboard

[web/kit/lib/catalog/time.ts](../../web/kit/lib/catalog/time.ts) gains a second `ModuleReply` entry (`key: 'lookup'`, `messageKey: 'lookupMessage'`), rendered by the existing generic reply inspector at `web/dashboard/src/routes/(app)/modules/[id]/+page.svelte`: that page iterates `def.replies` with no per-module branch, so a two-reply module renders two `ReplyRow`/`ReplyEditor` cards without new dashboard code. `buildConfig`/`allowedConfigKeys` in `+page.server.ts` key off `reply.messageKey` generically, so `lookupMessage` persists into the module's config blob the same way `message` does today.

The `{place}` token is scoped to this reply's own token palette (`ModuleReply.tokens`/`previewSamples`, per [module-def.ts](../../web/kit/lib/catalog/module-def.ts)); it is not part of the shared custom-command Variable manifest at [web/kit/lib/variables](../../web/kit/lib/variables), since module reply surfaces are wired into that manifest in a later phase (see `web/kit/lib/variables/surfaces.ts`).

Copy: [web/kit/lib/i18n/locales/en.json](../../web/kit/lib/i18n/locales/en.json) and [fr.json](../../web/kit/lib/i18n/locales/fr.json), `modules.catalog.time.description` and `modules.catalog.time.replies.lookup.tagline`.

## 7. Code layout

| Path | Purpose |
| --- | --- |
| [pkg/tzname/tzname.go](../../pkg/tzname/tzname.go) | `Resolve(query) (Match, bool)`, the package's public surface (D14). |
| [pkg/tzname/normalize.go](../../pkg/tzname/normalize.go) | Argument normalization (D11). |
| [pkg/tzname/offset.go](../../pkg/tzname/offset.go) | UTC offset grammar (D12). |
| [pkg/tzname/curated.go](../../pkg/tzname/curated.go) | Curated city aliases and abbreviation table (D2, D3). |
| [pkg/tzname/index.go](../../pkg/tzname/index.go) | City-segment index over canonical zones (D9). |
| [pkg/tzname/zones.go](../../pkg/tzname/zones.go) | Canonical/link zone classification. |
| [pkg/tzname/zones_gen.go](../../pkg/tzname/zones_gen.go) | Generated IANA zone list, golden-tested. |
| [pkg/tzname/gen/main.go](../../pkg/tzname/gen/main.go) | Generator that produces `zones_gen.go` from the Go toolchain's zoneinfo.zip. |
| [pkg/tzname/internal/zonelist](../../pkg/tzname/internal/zonelist/zonelist.go) | Shared zone-list types between the generator and the package. |
| [app/twitch/sesame/engine/time_config.go](../../app/twitch/sesame/engine/time_config.go) | `LookupMessage`, `Zone` read via `tzname.Load`. |
| [app/twitch/sesame/modules/timeofday.go](../../app/twitch/sesame/modules/timeofday.go) | `!time` handler: home time and place lookup. |
| [internal/domain/i18n/locales/en.json](../../internal/domain/i18n/locales/en.json), [fr.json](../../internal/domain/i18n/locales/fr.json) | `time.unset`, `time.unavailable`, `time.unknown` (D13). |
| [web/kit/lib/catalog/time.ts](../../web/kit/lib/catalog/time.ts) | Dashboard module definition, second reply. |
| [web/kit/lib/i18n/locales/en.json](../../web/kit/lib/i18n/locales/en.json), [fr.json](../../web/kit/lib/i18n/locales/fr.json) | Dashboard copy. |

## 8. Tests

- `pkg/tzname`: offset grammar (valid and rejected shapes, D12); full IANA name any case; curated aliases including the declared abbreviation meanings (D3); city-segment index; zero unresolved collisions between canonical zones (D9); `Etc/*` zones never resolve.
- `app/twitch/sesame/modules/timeofday_test.go`: home time unchanged; lookup by city, IANA name, abbreviation, offset; unknown place hint (D4); lookup with home zone unset (D5); cooldown consumed on both hit and miss.
- `web/kit/lib/catalog`: `time` module def has two replies, `lookup` keyed by `lookupMessage`, tokens include `place`.
- `web/kit/lib/i18n`: literal-keys/parity tests pick up `replies.lookup.tagline` in both locales.

## 9. Out of scope

- `{time:<place>}` as a custom-command template variable (D8).
- A Discord `!time` command (D8).
- Importer mapping of Nightbot/Fossabot `$(time <tz>)` (D8).
- A broadcaster opt-out toggle for place lookup (D7).
