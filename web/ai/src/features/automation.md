---
title: Chat automation and stream management modules: reference for AI assistants
description: Triggers, chat alerts, Premium beta AutoMod, emote celebrations, personality, time, and stream commands.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/modules/
---

[Human guide](https://itsbagelbot.com/guides/modules/).

ItsBagelBot can respond to ordinary chat, announce Twitch events, celebrate emote patterns, report local time, and manage stream metadata.
AutoMod is a Premium beta module.

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

- Plan: {{website:pricingTiers.free}} for triggers, alerts, emoteplay, personality, time, and stream commands. {{console:modules.catalog.automod.label}} is {{website:pricingTiers.premium}} beta.
- External prerequisites: Twitch authorization; alerts require the corresponding Twitch events. Stream writes require `channel:manage:broadcast`; ads require `channel:edit:commercial`.
- Dashboard destinations under "{{console:nav.modules}}": [{{console:modules.catalog.triggers.label}}](https://dashboard.itsbagelbot.com/modules/triggers), [{{console:modules.catalog.alerts.label}}](https://dashboard.itsbagelbot.com/modules/alerts), and [{{console:modules.catalog.automod.label}}](https://dashboard.itsbagelbot.com/modules/automod).
- Also configure [{{console:modules.catalog.emoteplay.label}}](https://dashboard.itsbagelbot.com/modules/emoteplay), [{{console:modules.catalog.personality.label}}](https://dashboard.itsbagelbot.com/modules/personality), and [{{console:modules.catalog.time.label}}](https://dashboard.itsbagelbot.com/modules/time).
- Stream commands are configured on [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands); each built-in can be disabled individually.
- Triggers default off. Rules match whole words by default, or contains, exact message, or starts-with. The first matching rule wins.
- Trigger replies support `{user}`, `{random}`, and `{choice:a,b,c}`. Each incoming message produces at most one trigger reply.
- Chat alerts default on for follows, subscriptions, gift subs, cheers, and raids. Ad-break alerts default off.
- A follow alert fires at most once per viewer every three days. Gift-sub alerts send one line per gifter.
- AutoMod's catalog default is on, subject to the Premium beta gate. Default enforcement is moderate; profanity filtering is off at that level.
- AutoMod can warn, delete, timeout, or ban. VIPs, moderators, and the broadcaster are exempt. Borderline content is left to human moderators.
- The safety floor for hate slurs and IP-grabber links remains at every level, including when optional AutoMod enforcement is off.
- Emoteplay defaults off. It recognizes clean emote pyramids and runs of single-emote messages. These reactions use ordinary chat events.
- Personality defaults on and reacts to praise, insults, pets, feeds, and mentions. Its script is built in.
- Local time defaults off. Save an IANA timezone and choose a 12- or 24-hour clock. Browser timezone suggestions are stored only after saving.
- Stream commands ship on. Title, category, and tags default to 5-second cooldowns, commercials to 30 seconds, and markers to 10 seconds.
- Triggers, chat alerts, AutoMod, and emoteplay register no chat command syntax or aliases for their automatic actions.
- Follow alert example from the catalog: "{{console:modules.catalog.alerts.replies.follow.defaultMessage}}".
- Gift alert example: "{{console:modules.catalog.alerts.replies.gift.defaultMessage}}".
- Ad-break alert example: "{{console:modules.catalog.alerts.replies.ads.defaultMessage}}".

</facts>

<commands>

<command name="!bagels">

### `!bagels`

- Syntax: `!bagels`
- Aliases: `!fed`, `!bagelcount`
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Shows the channel's bagel feeding count and its rank among channels.
- Example reply template: "{{chat:feed.rank}}".

</command>

<command name="!bagelboard">

### `!bagelboard`

- Syntax: `!bagelboard`
- Aliases: `!feedboard`, `!bagellb`
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Shows the bagel feeding leaderboard and channel standing.
- Example reply template: "{{chat:feed.board}}".

</command>

<command name="!time">

### `!time`

- Syntax: `!time`, `!time <place>`
- Aliases: None registered.
- Who can run it: Everyone while the corresponding module is enabled.
- Behaviour: Local streamer time or time for a city, zone, or offset such as Tokyo, EST, or UTC+2.
- Example reply template: "{{console:modules.catalog.time.replies.time.defaultMessage}}".

</command>

<command name="!title">

### `!title`

- Syntax: `!title`, `!title <title>`, `!settitle <title>`
- Aliases: `!settitle`
- Who can run it: Lead moderators and the broadcaster by default; saved built-in access can change this.
- Behaviour: Reads or sets the title, at most 140 characters. Empty `!settitle` prints usage.
- Example reply template: "{{chat:stream.title.updated}}".

</command>

<command name="!game">

### `!game`

- Syntax: `!game`, `!game <name>`, `!setgame <name>`
- Aliases: `!setgame`
- Who can run it: Lead moderators and the broadcaster by default; saved built-in access can change this.
- Behaviour: Reads or sets the Twitch category using the first search match. Empty `!setgame` prints usage.
- Example reply template: "{{chat:stream.game.updated}}".

</command>

<command name="!tags">

### `!tags`

- Syntax: `!tags`, `!tags <tag1, tag2>`, `!settags <tag1, tag2>`
- Aliases: `!settags`
- Who can run it: Lead moderators and the broadcaster by default; saved built-in access can change this.
- Behaviour: Reads or replaces tags. Maximum 10 tags, at most 25 characters each.
- Example reply template: "{{chat:stream.tags.updated}}".

</command>

<command name="!commercial">

### `!commercial`

- Syntax: `!commercial`, `!commercial [30|60|90|120|150|180]`
- Aliases: `!ad`
- Who can run it: Lead moderators and the broadcaster by default; saved access can change this. Live stream and commercial grant required.
- Behaviour: Starts an ad break. Bare invocation uses 30 seconds.
- Example reply template: "{{chat:stream.commercial.ok}}".

</command>

<command name="!marker">

### `!marker`

- Syntax: `!marker`, `!marker <description>`
- Aliases: None registered.
- Who can run it: Lead moderators and the broadcaster by default; saved access can change this. Live stream and broadcast-management grant required.
- Behaviour: Drops a marker; optional description has a 140-character maximum.
- Example reply template: "{{chat:stream.marker.ok}}".

</command>

</commands>

<related>

- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/modules/).

</related>

</page>
