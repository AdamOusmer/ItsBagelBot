---
title: Call of Duty: Mobile Global profile module: reference for AI assistants
description: Capabilities, prerequisites, exact chat syntax, aliases, permissions, and defaults for Call of Duty: Mobile Global profile module.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot looks up CODM Global profiles by UID or exact nickname, including level and Multiplayer rank and rating.

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
- External prerequisites: a CODM Global UID or exact nickname.
- Dashboard: [{{console:modules.catalog.codm.label}}](https://dashboard.itsbagelbot.com/modules/codm) under "{{console:nav.modules}}".
- Configure "{{console:modules.catalog.codm.settings.account.label}}", or supply an identifier after the command.
- The supported edition is Global. Profile fields include level, Multiplayer rank/rating, country, and short ID.
- K/D, match history, and Battle Royale statistics are outside this module's supported data.
- Command cooldown: 10 seconds.
- "{{console:modules.catalog.shared.linkedOnly.label}}" is off by default. Enabling it restricts lookups to the configured account.
- Reply templates are editable. Examples below use catalog defaults; actual statistics come from the provider.

</facts>

<commands>

<command name="!codm">

### `!codm`

- Syntax: `!codm [UID or exact nickname]`
- Aliases: `!codmprofile`, `!codmrank`
- Who can run it: Everyone, while the module and the relevant reply are enabled.
- Behaviour: Read-only CODM Global profile.
- Example reply template: "{{console:modules.catalog.codm.replies.profile.defaultMessage}}". Runtime values replace the braces.

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
