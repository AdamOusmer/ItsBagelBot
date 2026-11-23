---
title: Giveaways, viewer queue, and engagement modules: reference for AI assistants
description: Item giveaways, points raffles, entry and claim commands, giveaway rules, viewer queue, raid shoutouts, quotes, counters, and timers.
updated: 2026-11-20
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot supports item giveaways, points raffles, a viewer play queue, automatic raid shoutouts, saved quotes, counters, and scheduled chat messages.

<page type="feature">

<audience>

Assistants explaining features, selecting a module for a Twitch task, or helping streamers configure it.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- Use the stated access rules and prerequisites. Distinguish automated events from typed commands.
- Reply templates are examples from catalog defaults or fixed localized messages; values replace the braces.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}} for every feature on this page.
- External prerequisites: Twitch authorization. Raid shoutouts need incoming raid events.
- Configure [{{console:modules.catalog.queue.label}}](https://dashboard.itsbagelbot.com/modules/queue) and [{{console:modules.catalog.shoutout.label}}](https://dashboard.itsbagelbot.com/modules/shoutout) under "{{console:nav.modules}}".
- Dedicated destinations: [{{console:modules.catalog.quotes.label}}](https://dashboard.itsbagelbot.com/quotes), [{{console:nav.counters}}](https://dashboard.itsbagelbot.com/counters), and [{{console:nav.timers}}](https://dashboard.itsbagelbot.com/timers).
- Giveaways, raffles, queue, shoutout, quotes, and timers default off. Counters are always available in the dashboard.
- Opening a second round is refused while a giveaway or raffle is taking entries. If both rounds are raffles, chat says "{{chat:raffle.open.already}}" Otherwise, chat says "Draw or cancel it first." A new round can take entries while an earlier round is drawing, waiting for claims, or paying winners.
- Entry is free for both kinds of draw. An open draw takes priority for `!join`. Use `!queue join` to join the viewer queue directly.
- The viewer queue is first come, first served. Closing joins preserves the waiting line. Listing shows ten entries with a 5-second cooldown.
- Quote add/edit permissions default to moderators. Reads have a 5-second cooldown; deleted numbers are retired.
- Counter scopes: channel, per viewer, per command, and per viewer plus command. Custom commands with "{{console:commandEditor.bumpCounter}}" and channel-points rewards can increment them; there is no built-in chat command for counters.
- A new timer defaults to 600 seconds, at least 15 chat lines within 10 minutes, enabled, and live only.
- Timers support an offline option, a per-stream firing cap, and an end date. A firing cap of 0 is unlimited.
- Timers, counters in the dashboard, and automatic raid shoutouts do not need viewer chat commands.
- Automatic shoutout reply example: "{{console:modules.catalog.shoutout.replies.shoutout.defaultMessage}}".
- "{{console:modules.catalog.shoutout.settings.native_shoutout.label}}" is off by default. It adds Twitch's native shoutout to the chat line when enabled.

</facts>

<a id="points-raffles"></a>
## Points raffles

- Configure "{{console:modules.catalog.raffle.label}}" in [{{console:modules.catalog.loyalty.label}} > {{console:loyalty.gamesTitle}}](https://dashboard.itsbagelbot.com/loyalty#games). Enable Loyalty and the Raffle module. Moderators and the broadcaster start rounds from Twitch chat. The Loyalty page shows accrual health and flags unpaid raffle payouts. Use `!raffle status` in chat for live raffle status.
- The default entry window is 60 seconds. The opening message states the winner count or the automatic winner rule.
- "{{console:modules.catalog.raffle.settings.midCountEnabled.label}}" is on by default. It posts the entry count and time left at halfway if anyone has entered.
- "{{console:modules.catalog.raffle.settings.remindEnabled.label}}" is on by default. Raffles open for at least 10 minutes can post reminders every 5 minutes.
- With `auto`, the draw picks one winner per 10 entries, rounded up. The count is capped by the prize and the winner limit. Every winner gets the same whole-point share, rounded down. For example, three winners splitting 5,000 points get 1,666 each.
- Raffles close on schedule even if the stream goes offline. The bot credits winners automatically at the draw, without a claim step, and announces paid winnings. If a credit is still pending, `!winner` can show its status while the raffle settles.

<a id="item-giveaways"></a>
## Item giveaways

- Open [{{console:nav.giveaways}}](https://dashboard.itsbagelbot.com/giveaways) under Play. Use "{{console:giveaways.startGiveaway}}" or start a giveaway from Twitch chat. The page lets you manage "{{console:giveaways.presets}}", see entries and refusals, and track winner slots and delivery.
- Configure the prize, winner count, and entry time. The defaults are one winner and three minutes to enter.
- "{{console:modules.catalog.giveaway.settings.claimSeconds.label}}" defaults to two minutes. Set it to 0 to turn claims off, or choose between 30 seconds and 15 minutes. When claims are on, a rerolled slot gets a fresh window.
- Enable "{{console:modules.catalog.giveaway.settings.rerollNoShows.label}}" to draw replacements when claims expire.
- When the stream goes offline during a giveaway, automatic drawing and claim countdowns pause. They resume when the stream is live again. A moderator can draw manually while offline.
- Draw history stores only giveaway winners, with claim and delivery status. When the channel's public page is enabled, it lists those winners, their prizes, and when they won.

<a id="giveaway-rules"></a>
## Giveaway eligibility

| When | Rules |
| --- | --- |
| At entry | Chat role, broadcaster and moderator exclusions, minimum points, and minimum lifetime watch time in the channel |
| At draw | Channel follow or minimum follow age, minimum Twitch account age, and recent giveaway wins |

- The broadcaster can never enter a giveaway. Excluding moderators is optional.
- Points and watch time rules require Loyalty. A giveaway using either rule cannot start while watch time accrual is stalled.
- A giveaway that requires points or watch time takes a private copy of standings when entries open. The copy provides the cutoff for that round and the public leaderboard has its own display controls.
- Once an entry is accepted, its join-time eligibility stands. A refused viewer can try again and be admitted if they qualify. Draw-time rules are checked on candidates picked as winners.
- Subscriber luck can give viewers with a subscriber or founder badge up to 3x entry weight. "{{console:giveaways.rule.skip_winners_days}}" excludes candidates at draw time if they won a giveaway in the channel within the configured number of days. Both settings are optional.
- "{{console:modules.catalog.giveaway.settings.refusalEnabled.label}}" is on by default. Refused joins are batched by reason in chat about every 10 seconds. Viewers can use `!check` for their own details.

<commands>

<command name="!raffle">

### `!raffle`

- Syntax: `!raffle [amount] [seconds]`, `!raffle status`, `!sraffle [amount] [seconds]`, `!mraffle [amount] [seconds] [winners|auto]`, `!raffle draw [winners|auto]`, `!raffle close`, `!raffle cancel`, `!cancelraffle`
- Aliases: `!singleraffle` for `!sraffle`, `!multiraffle` for `!mraffle`.
- Who can run it: Anyone can read status. Starting, drawing, closing, and cancelling require moderator access.
- Behaviour: Bare `!raffle` shows status to viewers. For moderators it uses "{{console:modules.catalog.raffle.settings.bareRaffle.label}}". The default is "{{console:modules.catalog.raffle.settings.bareRaffle.options.single}}". Choose "{{console:modules.catalog.raffle.settings.bareRaffle.options.split}}" for a three-winner pot. `!raffle` with an amount and `!sraffle` open a single-winner round.
- Omit the amount when starting a raffle to use the configured prize, which defaults to 1,000 points. Amounts accept `k` and `m`. Raffle prizes reject `half` and `all`. Put a space between the amount and the time.
- `!mraffle` defaults to three winners. For a 5,000-point raffle open for 60 seconds, use `!mraffle 5k 60 3` or `!mraffle 5k 60 auto`.
- `!raffle close` draws early using the round's winner rule. `!raffle draw` also accepts a new winner count or `auto`. Cancel stops the round before a draw.
- Example reply template: "{{console:modules.catalog.raffle.replies.opened.defaultMessage}}".

</command>

<command name="!giveaway">

### `!giveaway`

- Syntax: `!giveaway`, `!giveaway status`, `!giveaway start [preset] [prize]`, `!giveaway draw`, `!giveaway cancel`
- Who can run it: Anyone can use bare `!giveaway` for the open giveaway or last winners. Moderators can also use the subcommands.
- Behaviour: The first word after `start` selects a preset if it matches a saved preset. Otherwise the full text is the prize and the default preset applies. Omit the prize text to use the saved default prize. For example, `!giveaway start regulars Signed poster` uses the `regulars` preset if it exists.
- `!giveaway draw` closes entries and draws winners now. `!giveaway cancel` cancels the open giveaway. Once the draw has started, use "{{console:giveaways.endSlots}}" on the dashboard to end unfinished slots.
- Example reply template: "{{console:modules.catalog.giveaway.replies.opened.defaultMessage}}".

</command>

<command name="!join">

### `!join`

- Syntax: `!join`
- Aliases: `!enter` and `!ticket` while a draw is open. `!raffle join` enters a raffle directly.
- Who can run it: Viewers who meet the entry requirements for the open draw.
- Behaviour: Enters the open giveaway or raffle once, for free. An open draw takes priority over the viewer queue. Successful `!join` entries are silent by default.
- On a viewer's first entry with `!enter` or `!ticket`, the bot replies "{{chat:draw.join.free}}"

</command>

<command name="!check">

### `!check`

- Syntax: `!check`
- Who can run it: Viewers while the draw is accepting entries. The cooldown is 10 seconds per viewer.
- Behaviour: Reports the caller's eligibility without entering them. It reports join-time rules first, then draw-time rules. Draw-time eligibility is checked again if the viewer is picked.
- Example reply template: "{{chat:eligibility.check.ok}}".

</command>

<command name="!claim">

### `!claim`

- Syntax: `!claim`
- Aliases: None registered.
- Who can run it: A giveaway winner with a claim window still open.
- Behaviour: Confirms the giveaway winner's slot before the deadline. It applies only when the giveaway has a claim window.
- Example reply template: "{{chat:draw.claim.ok}}".

</command>

<command name="!reroll">

### `!reroll`

- Syntax: `!reroll [slot|@user]`
- Who can run it: Moderators and the broadcaster.
- Behaviour: Bare `!reroll` replaces all eligible no-shows in the most recent settling giveaway. Use `!reroll 2` for slot 2 or `!reroll @user` for a specific candidate. A targeted slot can be rerolled after a missed claim or when verification stalls.
- A successful reroll has no command reply. If the winner line is enabled, the replacement winner announcement follows. With claims enabled, the default winner line is "{{chat:giveaway.won.claim}}"
- While the claim window is still open, the refusal is "{{chat:giveaway.action.claim_pending}}"

</command>

<command name="!winner">

### `!winner`

- Syntax: `!winner`
- Aliases: None registered.
- Who can run it: Everyone.
- Behaviour: Shows winner status for the latest settling round. For a points raffle this can include pending payouts. When nothing is settling, it can show the last recorded giveaway winners.
- Example reply template: "{{chat:draw.status.winners}}".

</command>

<command name="!queue">

### `!queue`

- Syntax: `!queue`, `!queue join`, `!queue leave`, `!queue list`
- Aliases: `!join` when no draw is open, `!leave`, `!list`, `!queuelist`
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Shows queue status, joins, leaves, or lists the next ten viewers. `!join` is the queue shortcut when no giveaway or raffle is open.
- Example reply template: "{{console:modules.catalog.queue.replies.join.defaultMessage}}".

</command>

<command name="!queue open">

### `!queue open`

- Syntax: `!queue open`, `!queue close`, `!queue next`, `!queue remove <user>`, `!queue clear`
- Aliases: None registered.
- Who can run it: Moderators, lead moderators, and the broadcaster.
- Behaviour: Opens or closes joins, calls the next viewer, removes a viewer, or empties the line.
- Example reply template: "{{console:modules.catalog.queue.replies.next.defaultMessage}}".

</command>

<command name="!quote">

### `!quote`

- Syntax: `!quote`, `!quote <number>`, `!quote <word>`
- Aliases: `!quotes`
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Random quote, numbered quote, or random matching quote.
- Example reply template: "{{chat:quote.show}}".

</command>

<command name="!addquote">

### `!addquote`

- Syntax: `!addquote <text>`, `!quote "text"`
- Aliases: `!quoteadd`
- Who can run it: Defaults to moderators; the streamer can configure who may add.
- Behaviour: Saves a numbered quote and its date.
- Example reply template: "{{chat:quote.added}}".

</command>

<command name="!quote edit">

### `!quote edit`

- Syntax: `!quote edit <number> <text>`
- Aliases: None registered.
- Who can run it: Defaults to moderators; the streamer can configure who may edit.
- Behaviour: Rewrites text while preserving the quote number and save date.
- Example reply template: "{{chat:quote.edited}}".

</command>

<command name="!quote remove">

### `!quote remove`

- Syntax: `!quote remove <number>`
- Aliases: None registered.
- Who can run it: Moderators, lead moderators, and the broadcaster.
- Behaviour: Deletes a quote. Its number is never reused.
- Example reply template: "{{chat:quote.removed}}".

</command>

<command name="Counting from chat">

### Counting from chat

- Syntax: a custom command such as `!deaths` with "{{console:commandEditor.bumpCounter}}" turned on and a counter chosen. A moderator may add a signed number: `!deaths 5` or `!deaths +5` adds 5, `!deaths -1` removes 1.
- Aliases: None. There is no built-in `!counter` command; a channel may create its own custom command with that name.
- Who can run it: Anyone triggers the custom command and adds 1. Only moderators, lead moderators, and the broadcaster can set the step; a number typed by anyone else is ignored and the command still adds 1.
- Behaviour: Each use adds 1 unless a moderator gives a step. A zero, a non-number, or a number beyond 1,000,000 in either direction falls back to 1. `{counter:name}` and `{count:name}` in a reply only read the value and never change it.
- Dashboard: Create, set, reset, rename, and delete counters on the [{{console:nav.counters}}](https://dashboard.itsbagelbot.com/counters) page. Channel-point rewards can also bump a counter from the Channel Points page.

</command>

</commands>

<related>

- [Loyalty and games](loyalty.md).
- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
