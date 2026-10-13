---
title: Hypixel Bedwars and Urchin module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Hypixel Bedwars and Urchin module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot provides Hypixel Bedwars sessions and lifetime statistics, plus Urchin sniper scores and blacklist tags.

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
- External prerequisites: a Minecraft account with data available through urchin.gg.
- Dashboard: [{{console:modules.catalog.urchin.label}}](https://dashboard.itsbagelbot.com/modules/urchin) under "{{console:nav.modules}}".
- Configure "{{console:modules.catalog.urchin.settings.account.label}}". Blank falls back to the Twitch username.
- The dashboard stores the Minecraft UUID when Mojang permits it; chat lookups are sent by username.
- Daily, weekly, and monthly sessions use provider reset windows. They are distinct from per-stream statistics.
- Commands have a 5-second cooldown. Blacklist tags describe provider records, not an assistant's independent judgment.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. Enabling it restricts lookups to the configured account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!daily">

### `!daily`

- Syntax: `!daily [player]`
- Aliases: `!bwdaily`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Bedwars session since the daily reset.
- Example reply template: "{{console:modules.catalog.urchin.replies.daily.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!weekly">

### `!weekly`

- Syntax: `!weekly [player]`
- Aliases: `!bwweekly`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Bedwars session since the weekly reset.
- Example reply template: "{{console:modules.catalog.urchin.replies.weekly.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!monthly">

### `!monthly`

- Syntax: `!monthly [player]`
- Aliases: `!bwmonthly`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Bedwars session since the monthly reset.
- Example reply template: "{{console:modules.catalog.urchin.replies.monthly.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!bwstats">

### `!bwstats`

- Syntax: `!bwstats [player]`
- Aliases: `!bedwars`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Lifetime stars, wins, finals, FKDR, and beds broken.
- Example reply template: "{{console:modules.catalog.urchin.replies.stats.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!sniper">

### `!sniper`

- Syntax: `!sniper [player]`
- Aliases: `!urchin`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Urchin sniper score.
- Example reply template: "{{console:modules.catalog.urchin.replies.sniper.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!tag">

### `!tag`

- Syntax: `!tag [player]`
- Aliases: `!tags`, `!bwtags`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Active Urchin blacklist tags.
- Example reply template: "{{console:modules.catalog.urchin.replies.tags.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!tagdescription">

### `!tagdescription`

- Syntax: `!tagdescription [player]`
- Aliases: None registered.
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Blacklist tags with their recorded reasons.
- Example reply template: "{{console:modules.catalog.urchin.replies.tagdescription.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Module errors](../errors/modules.md).
- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
