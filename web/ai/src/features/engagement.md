---
title: Giveaways, viewer queue, and engagement modules: reference for AI assistants
description: Raffles, viewer queue, raid shoutouts, quotes, counters, timers, syntax, permissions, and defaults.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot supports timed raffle giveaways, a viewer play queue, automatic raid shoutouts, saved quotes, counters, and scheduled chat messages.

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
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}} for every feature on this page.
- External prerequisites: Twitch authorization. Raffles and viewer queues need only chat access; raid shoutouts need incoming raid events.
- Configure [{{console:modules.catalog.raffle.label}}](https://dashboard.itsbagelbot.com/modules/raffle), [{{console:modules.catalog.queue.label}}](https://dashboard.itsbagelbot.com/modules/queue), and [{{console:modules.catalog.shoutout.label}}](https://dashboard.itsbagelbot.com/modules/shoutout) under "{{console:nav.modules}}".
- Dedicated destinations: [{{console:modules.catalog.quotes.label}}](https://dashboard.itsbagelbot.com/quotes), [{{console:nav.counters}}](https://dashboard.itsbagelbot.com/counters), and [{{console:nav.timers}}](https://dashboard.itsbagelbot.com/timers).
- Raffle, queue, shoutout, quotes, and timers default off. Counters are always available in the dashboard.
- Raffles default to 10 minutes, 1 winner, and reminders every 5 minutes. A reminder value of 0 disables reminders.
- Each raffle entrant counts once, winners are selected uniformly, and the draw produces a verifiable receipt.
- Winners have 15 minutes to claim. `!winner` has a 5-second cooldown.
- An active raffle takes `!join` priority. Use `!queue join` explicitly when both features are in use.
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

- Syntax: `!raffle status`, `!raffle [amount] [seconds]`, `!sraffle [amount] [seconds]`, `!mraffle [amount] [seconds]`, `!raffle draw [winners]`, `!raffle close`, `!raffle cancel`, `!cancelraffle`
- Aliases: `!singleraffle` for `!sraffle`, `!multiraffle` for `!mraffle`.
- Who can run it: Anyone reads status (a viewer typing `!raffle` gets it). Starting, draw, close, and cancel require moderator access.
- Behaviour: Runs a timed giveaway. `!raffle` and `!sraffle` give one winner the whole amount; `!mraffle` splits it across winners set by entrant count at the draw. Amounts take k and m (5k). Close draws early; cancel stops without drawing.
- Example reply template: "{{console:modules.catalog.raffle.replies.opened.defaultMessage}}".

</command>

<command name="!join">

### `!join`

- Syntax: `!join`
- Aliases: None registered.
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Enters an active raffle once. When a raffle is running, it takes priority over the viewer queue.
- Example reply template: "{{console:modules.catalog.raffle.replies.joined.defaultMessage}}".

</command>

<command name="!claim">

### `!claim`

- Syntax: `!claim`
- Aliases: None registered.
- Who can run it: A selected raffle winner within the claim window.
- Behaviour: Confirms a raffle prize.
- Example reply template: "{{console:modules.catalog.raffle.replies.claimOk.defaultMessage}}".

</command>

<command name="!winner">

### `!winner`

- Syntax: `!winner`
- Aliases: None registered.
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Recalls the latest draw and confirmations.
- Example reply template: "{{console:modules.catalog.raffle.replies.won.defaultMessage}}".

</command>

<command name="!queue">

### `!queue`

- Syntax: `!queue`, `!queue join`, `!queue leave`, `!queue list`
- Aliases: `!join (queue join when an active raffle does not claim it)`, `!leave`, `!list`, `!queuelist`
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Shows queue status, joins, leaves, or lists the next ten viewers. `!join` is the queue shortcut when no active raffle takes it.
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
