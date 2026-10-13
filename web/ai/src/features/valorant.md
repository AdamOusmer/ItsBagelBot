---
title: Valorant stats module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Valorant stats module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/valorant-stats/
---

[Human guide](https://itsbagelbot.com/valorant-stats/).

ItsBagelBot returns Valorant competitive rank, recent ranked matches, account details, regional leaderboards, and skin rotation in Twitch chat.

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
- External prerequisites: player lookups require a Riot ID with both name and #tag.
- Dashboard: [{{console:nav.modules}}](https://dashboard.itsbagelbot.com/modules), then [{{console:modules.catalog.valorant.label}}](https://dashboard.itsbagelbot.com/modules/valorant).
- Set "{{console:modules.catalog.valorant.settings.account.label}}" to `Name#Tag`. Name-only lookup is unsupported.
- "{{console:modules.catalog.valorant.settings.region.label}}" accepts `na`, `eu`, `ap`, `kr`, `br`, or `latam`. Blank detects the shard from the account.
- "{{console:modules.catalog.valorant.settings.platform.label}}" defaults to PC. PC and console ranks use separate ladders.
- Leaderboards show the regional top 10; the shop is the provider's global rotation, not an authenticated personal shop.
- Region and ladder tokens can appear anywhere in the lookup arguments. Shop lookup needs no linked player.
- Commands have a 10-second cooldown.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. Enabling it restricts lookups to the configured account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!val">

### `!val`

- Syntax: `!val [Name#Tag] [region] [pc|console]`, `!val rank [Name#Tag] [region] [pc|console]`
- Aliases: `!valrank`, `!val standing`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Competitive tier, RR, latest RR change, and peak tier.
- Example reply template: "{{console:modules.catalog.valorant.replies.rank.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val matches">

### `!val matches`

- Syntax: `!val matches [Name#Tag] [region] [pc|console]`
- Aliases: `!valmatches`, `!valhistory`, `!val match`, `!val history`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Recent ranked games with agent, K/D/A, result, and map.
- Example reply template: "{{console:modules.catalog.valorant.replies.matches.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val account">

### `!val account`

- Syntax: `!val account [Name#Tag] [region]`
- Aliases: `!valaccount`, `!valwho`, `!val who`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Resolved account and account level.
- Example reply template: "{{console:modules.catalog.valorant.replies.account.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val lb">

### `!val lb`

- Syntax: `!val lb [region] [pc|console]`
- Aliases: `!vallb`, `!valleaderboard`, `!val leaderboard`, `!val top`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Regional leaderboard; configure or supply a region.
- Example reply template: "{{console:modules.catalog.valorant.replies.board.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val shop">

### `!val shop`

- Syntax: `!val shop`
- Aliases: `!valshop`, `!valrotation`, `!val rotation`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Global skin rotation, VP prices, and reset countdown.
- Example reply template: "{{console:modules.catalog.valorant.replies.shop.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/valorant-stats/).

</related>

</page>
