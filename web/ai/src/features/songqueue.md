---
title: Spotify song requests module: reference for AI assistants
description: Spotify prerequisites, request commands, channel points, quotas, queue management, and default replies.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/song-requests/
---

[Human guide](https://itsbagelbot.com/song-requests/).

Viewers can request Spotify songs through Twitch chat or channel points. Both paths share the streamer's Spotify queue.
The bot also reads the current Spotify player and lets moderators manage requests.

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

- Plan: {{website:pricingTiers.free}}. Spotify Premium is a separate provider requirement.
- External prerequisites: your own Spotify app, a connected Premium Spotify account, and active playback on a device.
- The Spotify app owner also needs Premium. Spotify currently limits development-mode apps to 5 connected accounts; non-owners must be allowed users.
- Dashboard: [{{console:nav.songqueue}}](https://dashboard.itsbagelbot.com/songqueue).
- Module, chat requests, and reward requests start off. Turn on "{{console:spotify.index.masterLabel}}" and the desired request path.
- Chat uses "{{console:spotify.sr.enableLabel}}". The reward path uses "{{console:spotify.redeem.enableLabel}}" and requires Twitch channel-points access.
- "{{console:spotify.index.liveOnlyLabel}}" is on by default. The two request paths can allow offline requests independently.
- "{{console:spotify.sr.permLabel}}" selects everyone, subscribers, VIPs, moderators, or the broadcaster.
- "{{console:spotify.quota.title}}" has separate everyone, subscriber, VIP, and moderator limits. Blank means unlimited; the broadcaster is uncapped.
- Queue depth defaults to 100 and is capped by the handler at 1000. This is distinct from per-viewer quotas.
- `!sr`, current-player queries, and `!srlist` have a 5-second cooldown.
- Request input accepts song names, artist/song pairs, and supported Spotify track or album links.
- Default reward reply example: "{{console:modules.catalog.songqueue.replies.redeem.defaultMessage}}".

</facts>

<commands>

<command name="!sr">

### `!sr`

- Syntax: `!sr`, `!sr <song name, artist - song, or Spotify link>`
- Aliases: `!songrequest`, `!songreq`
- Who can run it: Bare status is for everyone. Adding songs uses "{{console:spotify.sr.permLabel}}", defaulting to everyone.
- Behaviour: Shows queue status or queues a Spotify track.
- Example reply template: "{{chat:songqueue.add.ok}}".

</command>

<command name="!current">

### `!current`

- Syntax: `!current`
- Aliases: `!song`, `!nowplaying`, `!np`
- Who can run it: Everyone.
- Behaviour: Reads the actual Spotify player, including songs nobody requested.
- Example reply template: "{{chat:songqueue.current.ok}}".

</command>

<command name="!srlist">

### `!srlist`

- Syntax: `!srlist`
- Aliases: `!songlist`
- Who can run it: Everyone.
- Behaviour: Shows the next five songs.
- Example reply template: "{{chat:songqueue.status.queued}}".

</command>

<command name="!remove">

### `!remove`

- Syntax: `!remove`, `!remove <position>`, `!sr remove [position]`, `!sr retract`, `!sr cancel`
- Aliases: None registered.
- Who can run it: Anyone can retract their own request. Removing a numbered position requires moderator access.
- Behaviour: Retracts the caller's request or removes a numbered queue entry.
- Example reply template: "{{chat:songqueue.retract.ok}}".

</command>

<command name="!skip">

### `!skip`

- Syntax: `!skip`, `!sr next`, `!sr skip`
- Aliases: `!next`
- Who can run it: Moderators, lead moderators, and the broadcaster.
- Behaviour: Requests the next Spotify track. The queue updates when playback changes.
- Example reply template: "{{chat:songqueue.next.pending}}".

</command>

<command name="!clear">

### `!clear`

- Syntax: `!clear`, `!sr clear`
- Aliases: None registered.
- Who can run it: Moderators, lead moderators, and the broadcaster.
- Behaviour: Empties the request queue.
- Example reply template: "{{chat:songqueue.cleared}}".

</command>

</commands>

<related>

- [Spotify setup](../spotify/setup.md), including `#step-1` through `#step-4`.
- [Spotify errors](../spotify/errors.md).
- [Connection errors](../errors/connection.md).
- [Human song requests page](https://itsbagelbot.com/song-requests/).
- [Human setup guide](https://itsbagelbot.com/guides/spotify-song-requests/).

</related>

</page>
