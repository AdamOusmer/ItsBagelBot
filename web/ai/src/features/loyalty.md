---
title: Loyalty and games: reference for AI assistants
description: Loyalty currency, leaderboard pause, points raffles, gamble odds, and pot or head-to-head duels.
updated: 2026-11-20
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot tracks channel loyalty currency earned from subscriptions, cheers, and live watch time.
Gamble and duels wager that balance. Points raffles credit winners in the same currency.

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
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}}. External prerequisites: Twitch channel access; viewers need accrued balances before wagering.
- Dashboard: [{{console:modules.catalog.loyalty.label}}](https://dashboard.itsbagelbot.com/loyalty). The [{{console:loyalty.gamesTitle}}](https://dashboard.itsbagelbot.com/loyalty#games) tab contains "{{console:modules.catalog.raffle.label}}", "{{console:modules.catalog.gamble.label}}", and "{{console:modules.catalog.duel.label}}" settings.
- Loyalty and the three game modules start off. Enable Loyalty to start games.
- This currency is separate from Twitch channel points. Its name is configurable under "{{console:loyalty.fieldName}}".
- Defaults: 500 points per new sub or resub, 100 per gifted sub to the gifter, 50 per 100 bits, and 10 per 5-minute watch tick.
- Sub, resub, and gifted sub awards multiply by 1, 2, or 6 for subscription tiers 1, 2, or 3. Watch ticks accrue while live.
- "{{console:loyalty.permSet}}", "{{console:loyalty.permAdjust}}", and "{{console:loyalty.permTransfers}}" control the corresponding capabilities.
- `!points` and `!watchtime` have 5-second cooldowns. `!leaderboard` has a 10-second cooldown and accepts 1 to 10 entries.
- Use "{{console:loyalty.freezeButton}}" in "{{console:loyalty.freezeTitle}}" on the Loyalty page or use `!freeze` in chat. The pause pins the public leaderboard, `!leaderboard`, and the dashboard's "{{console:loyalty.topTitle}}" list for up to 30 minutes. Points and watch time keep accruing. Balance commands, transfers, games, and giveaways continue to work. Resetting every viewer's points is refused while standings are paused.
- The private standings cutoff for a giveaway with a points or watch time rule is taken when that giveaway opens. It works independently of the public pause. See [giveaway eligibility](engagement.md#giveaway-rules).
- Start points raffles from Twitch chat. Bare `!raffle` uses the saved mode, which defaults to one winner. Use `!sraffle` for one winner or `!mraffle` for a split pot. Entry is free and the bot pays winners automatically. See [raffle commands and payout rules](engagement.md#points-raffles).
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
- Behaviour: Shows the caller's live points and watch time, including while the leaderboard is paused.
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
- Behaviour: Shows the channel's top point balances. During a leaderboard pause, it uses the standings from when the pause began.
- Example reply template: "{{chat:loyalty.leaderboard}}".

</command>

<command name="!freeze">

### `!freeze`

- Syntax: `!freeze [minutes]`, `!unfreeze`
- Who can run it: Moderators and the broadcaster while Loyalty is enabled.
- Behaviour: `!freeze` pauses displayed standings for 1 to 30 minutes. The default is 30 minutes. Running it again restarts the timer and takes a fresh copy of the standings. The paused leaderboard jumps to the current values. `!unfreeze` restores live standings immediately. The dashboard control is "{{console:loyalty.unfreezeButton}}".
- Example reply template: "{{chat:loyalty.freeze.started}}".

</command>

<command name="!gamble">

### `!gamble`

- Syntax: `!gamble <amount|half|all>`
- Aliases: None registered.
- Who can run it: Everyone with enough points, while loyalty and gamble are enabled.
- Behaviour: Rolls 1 to 100. A winning roll earns the stake amount. A losing roll deducts it. Amounts accept `k` and `m` within the configured bet limits.
- Example reply template: "{{console:modules.catalog.gamble.replies.won.defaultMessage}}".

</command>

<command name="!duel">

### `!duel`

- Syntax: `!duel`, `!duel <stake|half|all>`, `!duel @user <stake|half|all>`
- Aliases: None registered.
- Who can run it: Everyone with enough points, while loyalty and duels are enabled.
- Behaviour: Shows status, opens or joins a pot, or challenges a viewer to equal stakes. Amounts accept `k` and `m` within the configured stake limits.
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
