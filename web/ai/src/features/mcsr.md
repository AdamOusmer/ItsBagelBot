---
title: MCSR Ranked and PaceMan module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for MCSR Ranked and PaceMan module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot reports MCSR Ranked ratings, matches, leaderboards, race results, personal bests, and PaceMan speedrun splits.

<page type="feature">

<audience>

Assistants answering game-stat questions or helping a streamer configure this module.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- Treat sample replies as examples, not current player data.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}}. The module is off by default.
- External prerequisites: a Minecraft account with MCSR Ranked records or PaceMan data for the requested lookup.
- Dashboard: [{{console:modules.catalog.mcsr.label}}](https://dashboard.itsbagelbot.com/modules/mcsr) under "{{console:nav.modules}}".
- Configure "{{console:modules.catalog.mcsr.settings.account.label}}". Blank falls back to the Twitch username.
- Ranked lookups can use a stored Minecraft UUID across renames. PaceMan still uses the username.
- Stream-session rating changes use the linked account and a snapshot at stream start.
- `!elo`, `!lastmatch`, `!record`, and `!lb` accept a trailing `season:11` for a past season.
- Leaderboards show the top 5. Commands have a 10-second cooldown.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. It restricts ordinary player lookups to the configured account. For `!record`, the first supplied opponent is retained and compared with the linked account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!elo">

### `!elo`

- Syntax: `!elo [player] [season:number]`
- Aliases: `!mcsr`, `!ranked`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Rating, rank, and season record.
- Example reply template: "{{console:modules.catalog.mcsr.replies.elo.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!session">

### `!session`

- Syntax: `!session`
- Aliases: `!mcsrsession`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Linked account rating change and record since stream start.
- Example reply template: "{{console:modules.catalog.mcsr.replies.session.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!pace">

### `!pace`

- Syntax: `!pace [player]`
- Aliases: `!pacesession`, `!splits`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: PaceMan session split averages and nethers per hour.
- Example reply template: "{{console:modules.catalog.mcsr.replies.pace.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!nethers">

### `!nethers`

- Syntax: `!nethers [player]`
- Aliases: `!nph`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: PaceMan session nether entries and entrance pace.
- Example reply template: "{{console:modules.catalog.mcsr.replies.nethers.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!lastfort">

### `!lastfort`

- Syntax: `!lastfort [player]`
- Aliases: `!lastpace`, `!previousfort`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Most recent run reaching a fortress or bastion.
- Example reply template: "{{console:modules.catalog.mcsr.replies.lastfort.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!lastmatch">

### `!lastmatch`

- Syntax: `!lastmatch [player] [season:number]`
- Aliases: `!rankedmatch`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Most recent Ranked opponent, result, seed, time, and rating change.
- Example reply template: "{{console:modules.catalog.mcsr.replies.lastmatch.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!record">

### `!record`

- Syntax: `!record <player> [player2] [season:number]`
- Aliases: `!matchrecord`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Head-to-head record. One name compares with the linked account.
- Example reply template: "{{console:modules.catalog.mcsr.replies.record.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!lb">

### `!lb`

- Syntax: `!lb [phase [predicted]|record] [country:xx] [season:number]`
- Aliases: `!rankedlb`, `!mcsrlb`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Elo, phase-point, or record leaderboard with optional country filter.
- Example reply template: "{{console:modules.catalog.mcsr.replies.lb.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!race">

### `!race`

- Syntax: `!race [player]`
- Aliases: `!weeklyrace`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Weekly race leader and the selected player's time.
- Example reply template: "{{console:modules.catalog.mcsr.replies.race.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!pb">

### `!pb`

- Syntax: `!pb [daily|weekly|monthly|ranked] [player]`
- Aliases: `!personalbest`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: PaceMan windowed or all-time personal best; ranked selects the Ranked season best.
- Example reply template: "{{console:modules.catalog.mcsr.replies.pb.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Module errors](../errors/modules.md).
- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
