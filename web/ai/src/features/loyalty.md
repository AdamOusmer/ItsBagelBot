---
title: Loyalty, gamble, and duel modules: reference for AI assistants
description: Loyalty currency, earning defaults, point permissions, gamble odds, and pot or head-to-head duels.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot tracks channel loyalty currency earned from subscriptions, cheers, and live watch time.
Gamble and duels spend that same balance when the streamer enables them.

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

- Plan: {{website:pricingTiers.free}}. External prerequisites: Twitch channel access; viewers need accrued balances before wagering.
- Dashboard: [{{console:modules.catalog.loyalty.label}}](https://dashboard.itsbagelbot.com/loyalty), with games under "{{console:loyalty.gamesTitle}}".
- Configure game settings at [{{console:modules.catalog.gamble.label}}](https://dashboard.itsbagelbot.com/modules/gamble) and [{{console:modules.catalog.duel.label}}](https://dashboard.itsbagelbot.com/modules/duel).
- Loyalty, gamble, and duel start off. Both games require loyalty to be on.
- This currency is separate from Twitch channel points. Its name is configurable under "{{console:loyalty.fieldName}}".
- Defaults: 500 points per new sub or resub, 100 per gifted sub to the gifter, 50 per 100 bits, and 10 per 5-minute watch tick.
- Sub, resub, and gifted sub awards multiply by 1, 2, or 6 for subscription tiers 1, 2, or 3. Watch ticks accrue while live.
- "{{console:loyalty.permSet}}", "{{console:loyalty.permAdjust}}", and "{{console:loyalty.permTransfers}}" control the corresponding capabilities.
- `!points` and `!watchtime` have 5-second cooldowns. `!leaderboard` has a 10-second cooldown and accepts 1 to 10 entries.
- Gamble defaults: 50% win chance, minimum bet 1, maximum bet 1000, and a 10-second per-viewer cooldown. Win chance allows 1 to 99.
- A pot duel uses stake-weighted selection and awards the whole pot. A challenge uses equal stakes and a fair coin flip.
- Duel defaults: minimum stake 1, maximum stake 1000, pot window 60 seconds, and challenge window 120 seconds.
- Declined, cancelled, and unanswered challenges refund the escrowed points.
- Moderator counter management is documented in [engagement](engagement.md).

</facts>

<commands>

<command name="!points">

### `!points`

- Syntax: `!points`
- Aliases: None registered.
- Who can run it: Everyone while loyalty is enabled.
- Behaviour: Shows the caller's points and watch time.
- Example reply template: "{{chat:loyalty.points}}".

</command>

<command name="!watchtime">

### `!watchtime`

- Syntax: `!watchtime`
- Aliases: None registered.
- Who can run it: Everyone while loyalty is enabled.
- Behaviour: Shows accrued watch time.
- Example reply template: "{{chat:loyalty.watchtime}}".

</command>

<command name="!points give">

### `!points give`

- Syntax: `!points give @user <amount>`
- Aliases: None registered.
- Who can run it: Viewers transfer their own points when viewer transfers are enabled; the broadcaster can always transfer.
- Behaviour: Moves points to another viewer. Self-transfers and insufficient funds are rejected.
- Example reply template: "{{chat:loyalty.points.gave}}".

</command>

<command name="!points set">

### `!points set`

- Syntax: `!points set <user> <amount>`
- Aliases: None registered.
- Who can run it: Broadcaster, or moderators when "{{console:loyalty.permSet}}" is enabled.
- Behaviour: Writes an exact balance.
- Example reply template: "{{chat:loyalty.points.adjusted}}".

</command>

<command name="!points add">

### `!points add`

- Syntax: `!points add <user> <amount>`, `!points remove <user> <amount>`
- Aliases: None registered.
- Who can run it: Broadcaster, or moderators when "{{console:loyalty.permAdjust}}" is enabled.
- Behaviour: Grants or removes points.
- Example reply template: "{{chat:loyalty.points.adjusted}}".

</command>

<command name="!leaderboard">

### `!leaderboard`

- Syntax: `!leaderboard [1-10]`
- Aliases: `!top`
- Who can run it: Everyone while loyalty is enabled.
- Behaviour: Shows the channel's top point balances.
- Example reply template: "{{chat:loyalty.leaderboard}}".

</command>

<command name="!gamble">

### `!gamble`

- Syntax: `!gamble <amount>`, `!gamble half`, `!gamble all`
- Aliases: None registered.
- Who can run it: Everyone with enough points, while loyalty and gamble are enabled.
- Behaviour: Rolls 1 to 100. A winning roll earns the stake amount; a losing roll deducts it.
- Example reply template: "{{console:modules.catalog.gamble.replies.won.defaultMessage}}".

</command>

<command name="!duel">

### `!duel`

- Syntax: `!duel`, `!duel <stake>`, `!duel @user <stake>`
- Aliases: None registered.
- Who can run it: Everyone with enough points, while loyalty and duels are enabled.
- Behaviour: Shows status, opens or joins a pot, or challenges a viewer to equal stakes.
- Example reply template: "{{console:modules.catalog.duel.replies.challenge.defaultMessage}}".

</command>

<command name="!duel accept">

### `!duel accept`

- Syntax: `!duel accept`, `!duel decline`
- Aliases: None registered.
- Who can run it: Only the challenged viewer can accept or decline. Acceptance requires enough points.
- Behaviour: Accept settles equal stakes by coin flip. Decline refunds the opener.
- Example reply template: "{{console:modules.catalog.duel.replies.won.defaultMessage}}".

</command>

<command name="!duel cancel">

### `!duel cancel`

- Syntax: `!duel cancel`
- Aliases: None registered.
- Who can run it: The opener or a moderator.
- Behaviour: Cancels the duel and refunds escrowed points.
- Example reply template: "{{chat:duel.cancel.ok}}".

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
