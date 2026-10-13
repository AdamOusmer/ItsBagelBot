---
title: Fortnite stats module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Fortnite stats module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot returns Fortnite Battle Royale lifetime, season, and stream-session stats, plus the daily item shop.

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
- External prerequisites: an Epic display name whose stats the provider can return.
- Dashboard: [{{console:modules.catalog.fortnite.label}}](https://dashboard.itsbagelbot.com/modules/fortnite) under "{{console:nav.modules}}".
- Set "{{console:modules.catalog.fortnite.settings.account.label}}" to the Epic display name. Blank falls back to the Twitch username.
- Only Epic-name lookups work. Console players can use their Epic name; PlayStation and Xbox name lookup is unsupported.
- Stream-session statistics use the linked account and a snapshot taken when the stream starts.
- Season rollovers are tracked automatically. Commands have a 10-second cooldown.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. Enabling it restricts lookups to the configured account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!fn">

### `!fn`

- Syntax: `!fn [player]`, `!fn stats [player]`
- Aliases: `!fnstats`, `!fortnitestats`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Lifetime wins, matches, kills, K/D, win rate, and solo/duo/squad wins.
- Example reply template: "{{console:modules.catalog.fortnite.replies.stats.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!fn season">

### `!fn season`

- Syntax: `!fn season [player]`
- Aliases: `!fnseason`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Current-season Battle Royale statistics.
- Example reply template: "{{console:modules.catalog.fortnite.replies.season.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!fn session">

### `!fn session`

- Syntax: `!fn session`
- Aliases: `!fnsession`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Changes for the linked account since the stream began.
- Example reply template: "{{console:modules.catalog.fortnite.replies.session.defaultMessage}}". Runtime values replace the braces.

</command>

<command name="!fn store">

### `!fn store`

- Syntax: `!fn store`
- Aliases: `!fnstore`, `!itemshop`, `!fnshop`, `!fn shop`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Daily item-shop rotation.
- Example reply template: "{{console:modules.catalog.fortnite.replies.store.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
