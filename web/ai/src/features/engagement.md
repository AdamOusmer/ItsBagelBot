---
title: Giveaways, viewer queue, and engagement modules: reference for AI assistants
description: Giveaways, points raffles, viewer queue, raid shoutouts, quotes, counters, timers, syntax, permissions, and defaults.
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
- External prerequisites: Twitch authorization. Raffles also require Loyalty to be enabled before starting. Raid shoutouts need incoming raid events.
- Configure [{{console:modules.catalog.raffle.label}}](https://dashboard.itsbagelbot.com/loyalty#games) under Loyalty Games. Open [{{console:modules.catalog.giveaway.label}}](https://dashboard.itsbagelbot.com/giveaways) under Play.
- Configure [{{console:modules.catalog.queue.label}}](https://dashboard.itsbagelbot.com/modules/queue) and [{{console:modules.catalog.shoutout.label}}](https://dashboard.itsbagelbot.com/modules/shoutout) under "{{console:nav.modules}}".
- Dedicated destinations: [{{console:modules.catalog.quotes.label}}](https://dashboard.itsbagelbot.com/quotes), [{{console:nav.counters}}](https://dashboard.itsbagelbot.com/counters), and [{{console:nav.timers}}](https://dashboard.itsbagelbot.com/timers).
- Giveaways, raffles, queue, shoutout, quotes, and timers default off. Counters are always available in the dashboard.
- A points raffle defaults to 1,000 points, 60 seconds, and one winner. Raffle winnings are paid automatically.
- An item giveaway is free to enter. Its optional claim window defaults to two minutes.
- An open giveaway or raffle takes `!join` priority. Use `!queue join` to join the queue directly.
- The viewer queue is first come, first served. Closing joins preserves the waiting line. Listing shows ten entries with a 5-second cooldown.
- Quote add/edit permissions default to moderators. Reads have a 5-second cooldown; deleted numbers are retired.
- Counter scopes: channel, per viewer, per command, and per viewer plus command. Custom commands with "{{console:commandEditor.bumpCounter}}" and channel-points rewards can increment them; there is no built-in chat command for counters.
- A new timer defaults to 600 seconds, at least 15 chat lines within 10 minutes, enabled, and live only.
- Timers support an offline option, a per-stream firing cap, and an end date. A firing cap of 0 is unlimited.
- Timers, counters in the dashboard, and automatic raid shoutouts do not need viewer chat commands.
- Automatic shoutout reply example: "{{console:modules.catalog.shoutout.replies.shoutout.defaultMessage}}".
- "{{console:modules.catalog.shoutout.settings.native_shoutout.label}}" is off by default. It adds Twitch's native shoutout to the chat line when enabled.

</facts>

<commands>

<command name="!raffle">

### `!raffle`

- Syntax: `!raffle status`, `!raffle [amount] [seconds]`, `!sraffle [amount] [seconds]`, `!mraffle [amount] [seconds] [winners|auto]`, `!raffle draw [winners|auto]`, `!raffle close`, `!raffle cancel`, `!cancelraffle`
- Aliases: `!singleraffle` for `!sraffle`, `!multiraffle` for `!mraffle`.
- Who can run it: Anyone reads status (a viewer typing `!raffle` gets it). Starting, draw, close, and cancel require moderator access.
- Behaviour: Runs a timed points raffle using the configured mode. `!sraffle` always draws one winner. `!mraffle` splits the points across three winners by default. With `auto`, the draw selects one winner per ten entries, rounded up, subject to prize and winner limits. Amounts take k and m (5k). Close draws early, and cancel stops without drawing.
- Example reply template: "{{console:modules.catalog.raffle.replies.opened.defaultMessage}}".

</command>

<command name="!join">

### `!join`

- Syntax: `!join`
- Aliases: `!enter`, `!ticket` while a draw is open.
- Who can run it: Viewers who meet the entry requirements for the open draw.
- Behaviour: Enters the open giveaway or raffle once. An open draw takes priority over the viewer queue.
- Example reply template: "{{console:modules.catalog.raffle.replies.joined.defaultMessage}}".

</command>

<command name="!claim">

### `!claim`

- Syntax: `!claim`
- Aliases: None registered.
- Who can run it: A giveaway winner with a claim window still open.
- Behaviour: {{console:modules.catalog.giveaway.commands.claim}}

</command>

<command name="!winner">

### `!winner`

- Syntax: `!winner`
- Aliases: None registered.
- Who can run it: Everyone.
- Behaviour: Shows the giveaway currently settling or the latest recorded giveaway winners. For a settling points raffle, it can show winners and pending payouts.
- Example reply template: "{{console:modules.catalog.raffle.replies.won.defaultMessage}}".

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

- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
