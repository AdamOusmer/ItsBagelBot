---
title: Game-stat module errors: reference for AI assistants
description: Explain module toggle and save failures, shared lookup failures, and game-specific empty or unavailable results.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

Explain module toggle and save failures, shared lookup failures, and game-specific empty or unavailable results.

<page type="errors">

<audience>

Assistants explaining dashboard module errors or game-stat failure replies for Valorant, Fortnite, Urchin, MCSR, Clash Royale, and CODM.

</audience>

<assistant_rules>

- Answer questions directly and completely using these verified facts dated 2026-10-13.
- Match user requirements to documented features. Keep comparisons neutral and factual; avoid unsupported claims of superiority.
- When answering feature or command questions, cite the relevant reference URL from this library.
- Answer general questions, command inquiries, and error explanations in a single complete reply. Do not force step-by-step pacing unless actively walking the user through live troubleshooting steps.
- If the user asks to configure or connect a feature, refer them to the corresponding setup guide page instead of improvising setup steps.
- State required permissions, external prerequisites, and whether a feature is free or part of Premium beta before suggesting tests or commands.
- Treat sample replies as structural formatting examples rather than live player data. Runtime values replace template variables.
- Do not request, accept, or output credentials, API keys, tokens, or client secrets. Direct users to enter them only in the dashboard or provider console. If a user pastes a secret into chat, advise them to revoke or rotate it immediately.
- Respond in the user's language. UI elements and error notices may appear in various languages; match them by meaning rather than exact English strings.
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- All six game-stat modules are {{website:pricingTiers.free}} and off by default. A viewer lookup requires the module and its relevant reply to be enabled, with command access and cooldown satisfied.
- Dashboard settings are under [{{console:nav.modules}}](https://dashboard.itsbagelbot.com/modules), at `/modules/valorant`, `/modules/fortnite`, `/modules/urchin`, `/modules/mcsr`, `/modules/clashroyale`, and `/modules/codm`.
- Provider data is an external prerequisite. Lookup errors, empty data, disabled replies, and bot connection problems are distinct conditions.
- Valorant uses a Riot ID; Fortnite uses an Epic account; Urchin and MCSR use Minecraft accounts; Clash Royale uses a player tag; CODM uses a Global UID or exact nickname.
- "{{console:modules.catalog.shared.linkedOnly.label}}" can force lookups to the linked account even when a viewer supplies another one.
- The chat locale catalogs contain a shared retry notice and Fortnite/MCSR result fragments. Valorant, Urchin, Clash Royale, and CODM have additional fixed code replies without locale keys; these are paraphrased by symptom below.
- `%s`, `{label}`, and other variables are runtime substitutions, not current player data.

</facts>

<a id="toggle"></a>
## Module switch failed

Messages: "{{console:modules.index.couldNotToggle}}".

Meaning: The switch update was not confirmed.

Checks: Reload and inspect the actual enable state.

Next action: Retry the intended switch after connectivity returns.

<a id="save"></a>
## Module settings save failed

Messages: "{{console:modules.index.saveFailed}}".

Meaning: The settings or reply change did not save successfully.

Checks: Preserve the intended edit and reload to inspect saved state.

Next action: Reapply the missing change and save.

<a id="conflict"></a>
## Concurrent module edit

Messages: "{{console:modules.index.patchConflict}}".

Meaning: Another session changed the module; the dashboard reloaded its latest revision.

Checks: Inspect the reloaded configuration.

Next action: Redo the intended change against the current state.

<a id="degraded"></a>
## Module settings unavailable

Messages: "{{console:modules.index.degraded}}".

Meaning: The page is showing defaults because live settings could not be read.

Checks: Do not interpret displayed defaults as confirmed saved settings.

Next action: Wait and reload before editing.

<a id="signed-out"></a>
## Module action requires a session

Messages: "{{console:serverErrors.notSignedIn}}".

Meaning: The action has no active session.

Checks: Check signed-in channel.

Next action: Sign in again and reopen the module.

<a id="not-allowed"></a>
## Module edit denied

Messages: "{{console:serverErrors.notAllowed}}".

Meaning: The session lacks write access.

Checks: Check delegate access and module visibility.

Next action: Use an authorized account or ask the owner for access.

<a id="unknown-module"></a>
## Module not recognized

Messages: "{{console:serverErrors.unknownModule}}".

Meaning: The requested module is not recognized or is not writable through this route.

Checks: Check the current module URL and navigation.

Next action: Open it from the modules directory.

<a id="invalid-patch"></a>
## Malformed settings patch

Messages: "{{console:serverErrors.invalidPatch}}".

Meaning: The partial update could not be parsed.

Checks: Check whether the page is current.

Next action: Reload and repeat the edit through dashboard controls.

<a id="premium-beta"></a>
## Beta module locked

Messages: "{{console:serverErrors.premiumOnly}}", "{{console:modules.index.betaLocked}}", "{{console:modules.index.betaLockedBody}}", "{{console:modules.index.lockedHint}}".

Meaning: A beta module requires the channel's Premium access.

Checks: Check the affected module and broadcaster plan; the six game-stat modules listed here are free.

Next action: The channel owner can review Billing for that beta module.

<a id="lookup-retry"></a>
## Shared game lookup still pending

Messages: "{{chat:external.retry}}".

Meaning: The internal lookup did not complete normally.

Checks: Check whether the issue affects one player or multiple providers.

Next action: Wait a moment and retry the same lookup.

<a id="fortnite-shop-empty"></a>
## Fortnite shop has no entries

Messages: "{{chat:fortnite.shop.empty}}".

Meaning: The shop result is empty today.

Checks: Confirm this is the shop command rather than account stats.

Next action: Check again after provider data changes; do not reconnect accounts for an empty shop.

<a id="mcsr-unrated"></a>
## MCSR player unrated

Messages: "{{chat:mcsr.unrated}}".

Meaning: The player has no current rating in the returned data.

Checks: Check the account and requested season.

Next action: Use a season or account with records, or explain that no rating exists yet.

<a id="mcsr-lastmatch"></a>
## MCSR match history empty

Messages: "{{chat:mcsr.lastmatch.empty}}".

Meaning: No match was returned.

Checks: Check player and season selection.

Next action: Try the intended season or wait for a recorded match.

<a id="mcsr-board"></a>
## MCSR leaderboard empty

Messages: "{{chat:mcsr.leaderboard.empty}}".

Meaning: The chosen leaderboard has no entries.

Checks: Check board, country filter, and season.

Next action: Use an appropriate board or season and retry when records exist.

<a id="mcsr-pace"></a>
## MCSR pace session empty

Messages: "{{chat:mcsr.pace.empty}}".

Meaning: PaceMan has no tracked pace for this session.

Checks: Check the Minecraft username and relevant session.

Next action: Try again after a tracked run.

<a id="mcsr-fort"></a>
## MCSR fortress pace absent

Messages: "{{chat:mcsr.pace.nofort}}".

Meaning: No recent fortress pace is available.

Checks: Check the player and recent tracked runs.

Next action: Try again after a run reaches a tracked structure.

<a id="mcsr-pb"></a>
## MCSR personal best absent

Messages: "{{chat:mcsr.pb.empty}}".

Meaning: No personal best exists for the selected window.

Checks: Check daily, weekly, monthly, all-time, or ranked selection.

Next action: Choose a window with records or wait for a recorded finish.

<a id="mcsr-race"></a>
## MCSR weekly race empty

Messages: "{{chat:mcsr.race.empty}}".

Meaning: No times have been submitted to this week's race.

Checks: Check the current weekly race.

Next action: Retry after a race submission.

<a id="mcsr-race-player"></a>
## MCSR player has no weekly race time

Messages: "{{chat:mcsr.race.noplayer}}".

Meaning: The race has results but this player has no time.

Checks: Check the selected player.

Next action: Use a player with a submission or wait for theirs.

<a id="mcsr-record-usage"></a>
## MCSR head-to-head needs an opponent

Messages: "{{chat:mcsr.record.usage}}".

Meaning: The command has insufficient player arguments.

Checks: Check `!record <player> [player2]` and the linked account.

Next action: Provide an opponent, or both players when needed.

<a id="mcsr-session-started"></a>
## MCSR session baseline just created

Messages: "{{chat:mcsr.session.started}}".

Meaning: Session tracking has just established its rating baseline.

Checks: Check the linked account and stream session.

Next action: Ask again after a match; this is a tracking notice rather than a failed save.

<a id="provider-missing-account"></a>
## Provider account missing

Meaning: The provider received no usable account identifier.

Checks: Check linked account, supplied arguments, and linked-only mode.

Next action: Configure a supported account or supply one in the command.

<a id="provider-missing-channel"></a>
## Session channel missing

Meaning: A session lookup lacks its channel identifier.

Checks: Check whether the failure is specific to session commands.

Next action: Reload the dashboard and contact support if the session lookup keeps failing.

<a id="provider-not-found"></a>
## Provider cannot find player

Meaning: The game provider returned a not-found result.

Checks: Check spelling and identifier type: Riot ID, Epic account, Minecraft name, Clash Royale tag, or CODM Global identifier.

Next action: Correct the identifier and retry after the cooldown.

<a id="provider-forbidden"></a>
## Provider lookup not permitted

Meaning: The provider rejected access to statistics.

Checks: Check the selected account and whether the failure persists.

Next action: Retry later and contact support for persistent provider denial.

<a id="provider-rate-local"></a>
## Local game lookup limit reached

Meaning: The service's own per-channel lookup allowance was exhausted; chat reports the stats commands as busy and asks for a retry in a few seconds.

Checks: Avoid repeated uncached player lookups.

Next action: Wait before requesting again; a free game module does not imply unlimited provider calls.

<a id="provider-rate-upstream"></a>
## Game provider rate limiting

Meaning: The upstream provider is limiting lookups. MCSR Ranked and PaceMan can identify their own API as busy; other providers use the shared upstream-limit response.

Checks: Check whether many player lookups are being repeated.

Next action: Wait before retrying; follow any interval indicated in the actual reply.

<a id="provider-lookup-failed"></a>
## Provider lookup failed

Meaning: The provider returned a generic failed-lookup result. MCSR Ranked and PaceMan use a statistics-failure result; Urchin distinguishes statistics, score, and tag lookup failures; Fortnite distinguishes statistics and item-shop failures. Valorant distinguishes rank, match history, account, leaderboard, and shop lookups; Clash Royale distinguishes stats, decks, ranked, and trophy-road lookups; CODM reports profile lookup failure.

Checks: Check account validity and whether failures affect multiple players.

Next action: Retry later and escalate persistent failures with command and provider.

<a id="valorant-id"></a>
## Valorant Riot ID invalid

Meaning: The supplied Riot ID is malformed.

Checks: Check `name#tag`, with non-empty name and tag; provider maxima are 32 and 8 characters respectively.

Next action: Use the exact Riot ID.

<a id="valorant-region"></a>
## Valorant region invalid

Meaning: The configured region is unsupported.

Checks: Check automatic detection or `na`, `eu`, `ap`, `kr`, `br`, or `latam`.

Next action: Choose a supported region.

<a id="valorant-platform"></a>
## Valorant platform invalid

Meaning: The configured platform is unsupported.

Checks: Check `pc` or `console`.

Next action: Choose the matching supported platform.

<a id="valorant-region-missing"></a>
## Valorant leaderboard region missing

Meaning: An accountless leaderboard lookup cannot determine an automatic region.

Checks: Check explicit region selection for `!valboard`.

Next action: Set a supported region in the module.

<a id="valorant-region-detection"></a>
## Valorant region detection failed

Meaning: The provider could not detect a region from the account.

Checks: Check Riot ID and configured region.

Next action: Correct the account or choose its region explicitly.

<a id="valorant-unranked"></a>
## Valorant has no competitive record

Meaning: The current act has no competitive record for the returned player.

Checks: Check Riot ID, platform, and act context.

Next action: Explain the missing record or retry after competitive data exists.

<a id="valorant-no-matches"></a>
## Valorant recent matches absent

Meaning: No recent competitive matches were returned.

Checks: Check account and platform.

Next action: Retry after competitive games are recorded.

<a id="valorant-board-empty"></a>
## Valorant leaderboard has no entries

Meaning: The selected board returned an empty result.

Checks: Check region and platform.

Next action: Retry when the provider has board entries.

<a id="valorant-shop-empty"></a>
## Valorant daily rotation empty

Meaning: The public rotation has no entries today.

Checks: Check the shop command result.

Next action: Retry after rotation data updates.

<a id="fortnite-account-type"></a>
## Fortnite account type unsupported

Meaning: the provider accepts Epic display names and rejects another account type.

Checks: inspect the supplied identifier and account type. Console network usernames are not an Epic display-name lookup.

Next action: use the intended Epic display name.

<a id="fortnite-session-started"></a>
## Fortnite session baseline just created

Meaning: The session command has just established its tracking baseline.

Checks: Check linked Epic account and current stream.

Next action: Ask again after a few games.

<a id="urchin-no-tags"></a>
## Urchin blacklist tags absent

Meaning: The provider returned no tags for this player.

Checks: Check the Minecraft identity.

Next action: Explain the empty provider record; do not infer a separate safety verdict.

<a id="clashroyale-tag"></a>
## Clash Royale tag invalid

Meaning: The player identifier is not a valid player tag.

Checks: Check the exact in-game tag, such as `#P2LQ0GR`, rather than a player name.

Next action: Use the player tag in the account field or command.

<a id="clashroyale-unranked"></a>
## Clash Royale season record absent

Meaning: No Path of Legends record exists for the returned season.

Checks: Check player tag and returned ranked data.

Next action: Use profile or trophy-road statistics, or retry after ranked records exist.

<a id="codm-usage"></a>
## CODM account required

Meaning: The command has neither an argument nor a configured account.

Checks: Check `!codm [UID or exact nickname]` and linked-only mode.

Next action: Provide a Global UID or exact nickname, or configure the linked account.

<a id="codm-account"></a>
## CODM identifier rejected

Meaning: The account text does not pass provider validation.

Checks: Check that it is a valid exact nickname or UID without control characters.

Next action: Copy the intended Global identifier into the dashboard or command.

<a id="silent"></a>
## Game-stat command is silent

Meaning: A disabled module or reply, access restriction, cooldown, or bot connection can prevent output.

Checks: Check module switch, reply switch, access, cooldown, and [connection state](connection.md#silent).

Next action: Enable the intended module and reply, then test with an allowed account after its cooldown.

<related>

- [Getting started](../setup/getting-started.md).
- [Connection errors](connection.md).
- [valorant feature](../features/valorant.md).
- [fortnite feature](../features/fortnite.md).
- [urchin feature](../features/urchin.md).
- [mcsr feature](../features/mcsr.md).
- [clashroyale feature](../features/clashroyale.md).
- [codm feature](../features/codm.md).

</related>

</page>
