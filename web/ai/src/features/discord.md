---
title: Discord server integration: reference for AI assistants
description: Premium beta Discord setup, server destinations, roles, slash commands, and operational defaults.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/discord-setup/
---

[Human guide](https://itsbagelbot.com/guides/discord-setup/).

ItsBagelBot connects Twitch and Discord with go-live and clip posts, welcome messages, private tickets, temporary voice rooms, logs, and crumb ranks.

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

- Plan: {{website:pricingTiers.premium}} beta. The catalog marks Discord off by default and as a dedicated dashboard section.
- External prerequisites: a Discord account and a server you own or can manage. The server picker requires Manage Server.
- Dashboard: [{{console:nav.discord}}](https://dashboard.itsbagelbot.com/discord). Connect an existing server or create one from the supplied template.
- After selecting a server, its dashboard lives at `https://dashboard.itsbagelbot.com/discord/<guildId>`.
- Subpages: "{{console:discord.nav.channels}}" (`/channels`), "{{console:discord.nav.roles}}" (`/roles`), "{{console:discord.nav.announcements}}" (`/announcements`), "{{console:discord.nav.community}}" (`/community`), "{{console:discord.nav.tickets}}" (`/tickets`), and "{{console:discord.nav.settings}}" (`/settings`).
- Bot permissions include moderation, Manage Roles, and Manage Channels. Administrator is not required.
- Go-live posts are edited when the stream ends. Clips, raids, gift subs, and milestone-sub announcements use their configured destinations.
- An unset destination skips that post. Welcome embeds are posted in the selected server channel rather than sent as direct messages.
- Goodbye messages and link guard are off by default. Link guard targets members who have no role yet.
- Chat XP awards 15 crumbs with a 60-second cooldown. Rank and daily results are embed cards.
- Temporary voice rooms need a configured hub and category. Empty rooms are deleted. A user limit of 0 is unlimited; the maximum is 99.
- Ticket settings include open tickets per member, ticket staff, archive category, panel appearance, and transcripts.
- Twitch moderator, VIP, and subscriber roles are not automatically assigned or synchronized to Discord by ItsBagelBot.
- Discord's own Twitch integration can synchronize subscribers for Affiliate or Partner accounts. It must be enabled separately in Discord.
- Discord replies are embed cards and fixed handler messages. The module catalog contains no editable default reply template.

</facts>

<commands>

<command name="/daily">

### `/daily`

- Syntax: `/daily`
- Aliases: None registered.
- Who can run it: Server members while crumb ranks are enabled.
- Behaviour: Claims the daily crumbs reward; the result is an embed card.

</command>

<command name="/rank">

### `/rank`

- Syntax: `/rank`, `/rank user:<member>`
- Aliases: None registered.
- Who can run it: Server members while crumb ranks are enabled.
- Behaviour: Shows the member's crumb rank card; defaults to the caller.

</command>

<command name="/ticket open">

### `/ticket open`

- Syntax: `/ticket open`
- Aliases: None registered.
- Who can run it: Server members while tickets are enabled and configured, subject to their open-ticket cap.
- Behaviour: Creates a private support ticket.

</command>

<command name="/ticket close">

### `/ticket close`

- Syntax: `/ticket close`, `/ticket add user:<member>`
- Aliases: None registered.
- Who can run it: The ticket opener or ticket staff/moderators, inside a ticket.
- Behaviour: Closes the ticket or adds another member.

</command>

<command name="/ticket claim">

### `/ticket claim`

- Syntax: `/ticket claim`, `/ticket panel`
- Aliases: None registered.
- Who can run it: Ticket staff or moderators.
- Behaviour: Claims the current ticket, or posts the ticket panel.

</command>

<command name="/voice">

### `/voice`

- Syntax: `/voice name name:<text>`, `/voice limit count:<0-99>`, `/voice lock`, `/voice unlock`
- Aliases: None registered.
- Who can run it: Temporary room owner or Discord moderation staff, inside a tracked temporary voice room.
- Behaviour: Renames the room, changes its user limit, or changes access.

</command>

<command name="/timeout">

### `/timeout`

- Syntax: `/timeout user:<member> minutes:<positive integer>`
- Aliases: None registered.
- Who can run it: Members with moderation permissions or configured Discord moderation-staff roles.
- Behaviour: Times out a member; ticket-desk roles alone do not grant moderation power.

</command>

<command name="/kick">

### `/kick`

- Syntax: `/kick user:<member>`
- Aliases: None registered.
- Who can run it: Members with moderation permissions or configured Discord moderation-staff roles.
- Behaviour: Kicks a server member.

</command>

<command name="/ban">

### `/ban`

- Syntax: `/ban user:<member>`
- Aliases: None registered.
- Who can run it: Members with moderation permissions or configured Discord moderation-staff roles.
- Behaviour: Bans a server member.

</command>

<command name="/purge">

### `/purge`

- Syntax: `/purge count:<2-100>`
- Aliases: None registered.
- Who can run it: Members with moderation permissions or configured Discord moderation-staff roles.
- Behaviour: Bulk-deletes recent messages from the current channel.

</command>

</commands>

<related>

- [Discord setup](../setup/discord.md), [Discord errors](../errors/discord.md).
- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/discord-setup/).

</related>

</page>
