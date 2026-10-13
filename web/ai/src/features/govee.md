---
title: Govee lights module: reference for AI assistants
description: Govee API prerequisites, channel-point colour rewards, defaults, and limits.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/govee/
---

[Human guide](https://itsbagelbot.com/govee/).

Viewers redeem Twitch channel points and type a colour to change a Govee light.
Set up the API key once, then create a reward for each light.

<page type="feature">

<audience>

Assistants answering feature questions, explaining chat commands, or helping streamers configure the bot.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply, using these facts and the current channel configuration.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- Reply templates below are examples from catalog defaults or fixed localized chat replies. Runtime values replace the braces.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}}. External prerequisites: a Govee Home account, supported colour-capable lights, and a Govee API key.
- Lights must be available through Govee's API. Bluetooth-only devices may not appear.
- Dashboard: [{{console:modules.catalog.govee.label}}](https://dashboard.itsbagelbot.com/govee).
- Paste the key only into "{{console:govee.keyFieldLabel}}". It is stored encrypted and never shown back.
- A new Govee key invalidates older keys for that account. Avoid replacing a working key without a reason.
- The module is off by default. Enable "{{console:govee.masterLabel}}" and create rewards linked to your lights.
- Reward creation requires Twitch channel-points access; reconnect when "{{console:govee.reconnect}}" appears.
- "{{console:govee.liveOnlyLabel}}" is on by default. Offline redemptions are refunded while it is on.
- Reward cost must be a whole number of at least 1. Cooldowns range from 0 seconds to 7 days.
- Input accepts an English colour name such as `blue` or a hex code such as `#00ccff`.
- `off` is available when "{{console:govee.allowOffLabel}}" is enabled.
- Provider rejection, rapid changes, and unreachable devices have dedicated refund messages in the error reference.

</facts>

<commands>

This module registers no Twitch chat commands or aliases. Viewers use the configured channel-points reward.

Example success reply template from the catalog: "{{console:modules.catalog.govee.replies.reply.defaultMessage}}".

</commands>

<related>

- [Govee setup](../govee/setup.md), including `#step-1` through `#step-4`.
- [Govee errors](../govee/errors.md).
- [Connection errors](../errors/connection.md).
- [Human Govee page](https://itsbagelbot.com/govee/).
- [Human setup guide](https://itsbagelbot.com/guides/govee-lights/).

</related>

</page>
