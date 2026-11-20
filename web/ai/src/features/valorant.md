---
title: Valorant stats module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Valorant stats module.
updated: 2026-11-19
kind: feature
human: https://itsbagelbot.com/valorant-stats/
---

[Human guide](https://itsbagelbot.com/valorant-stats/).

ItsBagelBot shares Valorant stats from the streamer's signed-in Riot account in Twitch chat.

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
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}}. The module is off by default.
- External prerequisites: the streamer links their Riot account with Riot Sign On.
- Dashboard: [{{console:nav.modules}}](https://dashboard.itsbagelbot.com/modules), then [{{console:modules.catalog.valorant.label}}](https://dashboard.itsbagelbot.com/modules/valorant).
- Enable the module and choose "{{console:modules.catalog.valorant.connection.signIn}}" on the Riot account card. Sign in on Riot's own page and approve access. Back in the dashboard, the card displays the linked name and region, for example "Linked as Frosty#EUW1 (eu)".
- Player stats come only from the streamer's signed-in Riot account. Choose "{{console:modules.catalog.valorant.connection.unlink}}" on the same card to remove the saved account. Chat stops showing the account's stats within a few minutes.
- "{{console:modules.catalog.valorant.settings.region.label}}" accepts `na`, `eu`, `ap`, `kr`, `br`, or `latam`. Blank detects the shard from the account.
- "{{console:modules.catalog.valorant.settings.platform.label}}" defaults to PC. PC and console ranks use separate ladders.
- Region and ladder tokens can appear anywhere in the supported command arguments.
- Commands have a 10-second cooldown.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!val">

### `!val`

- Syntax: `!val [region] [pc|console]`, `!val rank [region] [pc|console]`
- Aliases: `!valrank`, `!val standing`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Competitive tier, RR, latest RR change, and peak tier.
- Example reply template: "{{console:modules.catalog.valorant.replies.rank.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val record">

### `!val record`

- Syntax: `!val record [region] [pc|console]`
- Aliases: `!valrecord`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Ranked games played on the account, wins, and win rate.
- Example reply template: "{{console:modules.catalog.valorant.replies.record.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val matches">

### `!val matches`

- Syntax: `!val matches [region] [pc|console]`
- Aliases: `!valmatches`, `!valhistory`, `!val match`, `!val history`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Recent ranked games with agent, K/D/A, result, and map.
- Example reply template: "{{console:modules.catalog.valorant.replies.matches.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val account">

### `!val account`

- Syntax: `!val account [region]`
- Aliases: `!valaccount`, `!valwho`, `!val who`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Linked Riot account and account level.
- Example reply template: "{{console:modules.catalog.valorant.replies.account.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!val lb">

### `!val lb`

- Syntax: `!val lb [region] [pc|console]`
- Aliases: `!vallb`, `!valleaderboard`, `!val leaderboard`, `!val top`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: The top 10 on the official PC leaderboard for the current act. Players who hide their name on Riot's leaderboard appear as "Secret Agent".
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

- [Module errors](../errors/modules.md).
- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/valorant-stats/).

</related>

</page>
