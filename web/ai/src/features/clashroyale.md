---
title: Clash Royale stats module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Clash Royale stats module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot returns Clash Royale profiles, current decks, Path of Legends standing, and trophy-road records.

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
- External prerequisites: a Clash Royale player tag such as `#P2LQ0GR`. Name lookup is unsupported.
- Dashboard: [{{console:modules.catalog.clashroyale.label}}](https://dashboard.itsbagelbot.com/modules/clashroyale) under "{{console:nav.modules}}".
- Configure "{{console:modules.catalog.clashroyale.settings.account.label}}", or supply a tag in each lookup.
- Ranked results use Path of Legends, with legacy league-season data as a fallback for older records.
- Commands have a 10-second cooldown.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. Enabling it restricts lookups to the configured account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!cr">

### `!cr`

- Syntax: `!cr [#tag]`
- Aliases: `!crstats`, `!clashroyale`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Lifetime level, win/loss record, win rate, three-crown wins, and clan.
- Example reply template: "{{console:modules.catalog.clashroyale.replies.stats.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!cr decks">

### `!cr decks`

- Syntax: `!cr decks [#tag]`
- Aliases: `!crdecks`, `!crdeck`, `!cr deck`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Current battle deck and average elixir cost.
- Example reply template: "{{console:modules.catalog.clashroyale.replies.decks.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!cr ranked">

### `!cr ranked`

- Syntax: `!cr ranked [#tag]`
- Aliases: `!crranked`, `!crpol`, `!cr pol`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Path of Legends standing or legacy-season fallback.
- Example reply template: "{{console:modules.catalog.clashroyale.replies.ranked.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!cr road">

### `!cr road`

- Syntax: `!cr road [#tag]`
- Aliases: `!crroad`, `!crtrophy`, `!cr trophy`, `!cr trophies`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Trophies, best trophies, and arena.
- Example reply template: "{{console:modules.catalog.clashroyale.replies.road.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
