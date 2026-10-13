---
title: Discord connection and server setup: reference for AI assistants
description: Connect a Discord server, grant required access, and configure channels, roles, tickets, announcements, and community features.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/guides/discord-setup/
---

[Human guide](https://itsbagelbot.com/guides/discord-setup/).

Connect a Discord server, grant required access, and configure channels, roles, tickets, announcements, and community features.

<page type="setup">

<audience>

Assistants walking a broadcaster or a delegate with Discord access through server setup.

</audience>

<assistant_rules>

- Read the relevant section before guiding the user.
- Present one step at a time. Wait for user confirmation or a description of the result before continuing to the next step.
- Describe controls by their screen position and function rather than relying solely on exact English text, as dashboard labels may be translated. Do not guess button or field names.
- Follow only the fields, paths, and URLs documented on this page. If a setting or screen is not covered, state that clearly.
- Do not request, accept, or repeat secrets, API keys, tokens, or passwords. Direct the user to paste credentials exclusively into the designated dashboard field. If a secret is pasted into chat, instruct the user to revoke or rotate it immediately and enter only the replacement in the dashboard.
- If an error occurs during setup, match the error message to the corresponding error reference page and follow its remediation steps.
- Respond in the user's language and match dashboard messages semantically.
- If two attempts fail to resolve an issue, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Discord is {{website:pricingTiers.premium}} beta. Check the broadcaster channel plan before starting. A delegate needs Discord section access.
- Dashboard: [{{console:nav.discord}}](https://dashboard.itsbagelbot.com/discord). A connected server opens at `https://dashboard.itsbagelbot.com/discord/<guildId>`; substitute its actual server ID.
- The connecting Discord account must own the server or have Manage Server. Keep every bot permission requested by the authorization screen checked.
- Moderation requires Kick Members, Ban Members, and Timeout Members. Setup requires Manage Roles and Manage Channels. The install also requests posting, voice-room, audit-log and Change Nickname permissions; keep all of them checked. Administrator is not required. Posting destinations need Send Messages and Embed Links.
- Put the bot role above every role it manages. The bot cannot bypass Discord role hierarchy.
- Each server stores its own channels, roles, and settings. An unset posting destination skips that post.
- Twitch moderators, VIPs, and subscribers are not automatically assigned or synchronized to Discord by ItsBagelBot.
- Match setup failures against [Discord errors](../errors/discord.md).

</facts>

<a id="step-1"></a>
## Step 1: Open Discord and check access

1. Open https://dashboard.itsbagelbot.com/discord under the intended Twitch channel.
2. Check the channel's Premium beta access and that your account can manage the intended Discord server.
3. If acting as a delegate, confirm Discord section access with the owner.

Verification: the Discord section opens without a beta access notice. If access is blocked, use [Premium beta errors](../errors/discord.md#locked).

Ask the user to confirm the observed result before continuing.

<a id="step-2"></a>
## Step 2: Select and authorize a server

1. Select "{{console:discord.index.addCta}}" to start the Discord account authorization flow.
2. Use the Discord account that owns the server or has Manage Server. Approve the server-list access.
3. On "{{console:discord.pick.title}}", select the intended server with "{{console:discord.pick.cta}}". If the server needs repair, use "{{console:discord.pick.repairCta}}".
4. On Discord's installation confirmation screen, keep all requested permissions checked and authorize installation.

If a new server is wanted and the dashboard offers "{{console:discord.index.createCta}}", it opens the supplied Discord template. Create the server there, then return to the dashboard add-server flow.
If a server belongs to another Twitch channel's binding, its current channel owner must disconnect it first.
Verification: the intended server appears under "{{console:discord.index.serversTitle}}" and its server page opens. Use [server selection errors](../errors/discord.md#no-guilds) if it is missing.

Ask the user to confirm the observed result before continuing.

<a id="step-3"></a>
## Step 3: Verify bot permissions and role hierarchy

1. In Discord's server role settings, place the bot's role above the roles it will manage.
2. Confirm the requested moderation, role, and channel permissions remain granted.
3. Inspect the bot's ability to send messages and embed links in the intended posting channels, including category overrides.
4. If installation permissions were removed, use "{{console:discord.settings.reconnectCta}}" on the dashboard server Settings page, then "{{console:discord.pick.repairCta}}" beside that server and retain all requested permissions.

Verification: permission warnings clear, and the bot can access the intended destinations. Follow [permission errors](../errors/discord.md#forbidden) for a refusal.

Ask the user to confirm the observed result before continuing.

<a id="step-4"></a>
## Step 4: Create or adopt the server layout

1. Open "{{console:discord.nav.settings}}" at `/discord/<guildId>/settings`.
2. Use "{{console:discord.settings.setupCta}}" when missing channels and roles need to be created.
3. Review the returned layout. Existing servers can have recognized channels adopted and other destinations left to choose.

Setup adds missing resources and uses pinned role choices; it does not delete existing channels or roles. If you want setup to use existing roles, configure and pin them in the next step before running setup again.
Verification: inspect the server's actual channels and roles. If "{{console:discord.toast.refused}}" appears, choose remaining destinations on the Channels page.

Ask the user to confirm the observed result before continuing.

<a id="step-5"></a>
## Step 5: Map roles

1. Open "{{console:discord.nav.roles}}" at `/discord/<guildId>/roles`.
2. Choose "{{console:discord.roles.ownerLabel}}", "{{console:discord.roles.leadModLabel}}", and "{{console:discord.roles.modsLabel}}" for the intended staff.
3. Choose "{{console:discord.roles.memberLabel}}" and review "{{console:discord.roles.autoLabel}}" if joining members should receive that role.
4. Select VIP, Subscriber, and Regulars roles as needed. Pin existing roles if the next layout setup should adopt them.
5. Select "{{console:discord.index.save}}" and verify the saved mappings.

The bot creates tier roles but you assign them yourself. Discord's separate Twitch integration can synchronize subscribers only for an Affiliate or Partner account; it is configured in Discord.
Verification: each slot points to the intended existing role below the bot's role. A missing pinned role uses [this warning](../errors/discord.md#dropped-pins).

Ask the user to confirm the observed result before continuing.

<a id="step-6"></a>
## Step 6: Choose channel destinations

1. Open "{{console:discord.nav.channels}}" at `/discord/<guildId>/channels`.
2. Choose "{{console:discord.channels.liveLabel}}", "{{console:discord.channels.clipsLabel}}", and "{{console:discord.channels.welcomeLabel}}" for wanted posts.
3. Choose "{{console:discord.channels.logLabel}}" and any individual log destinations needed.
4. For temporary voice, set "{{console:discord.channels.voiceHubLabel}}" and "{{console:discord.channels.voiceCategoryLabel}}" together. Choose room naming, privacy, and user limit as needed; 0 is unlimited and the maximum is 99.
5. Review subscriber and VIP destinations if those areas are used, then select "{{console:discord.index.save}}".

Verification: saved destinations match the intended server channels and remain accessible to the bot. Empty destinations skip posts. Follow [blocked destination errors](../errors/discord.md#cannot-post) if the bot cannot post.

Ask the user to confirm the observed result before continuing.

<a id="step-7"></a>
## Step 7: Configure announcements

1. Open "{{console:discord.nav.announcements}}" at `/discord/<guildId>/announcements`.
2. Set "{{console:discord.announcements.liveLabel}}" and "{{console:discord.announcements.clipsLabel}}" as wanted.
3. Review "{{console:discord.announcements.allowLabel}}" and "{{console:discord.announcements.denyLabel}}". An empty allow list allows every category; a denied category wins.
4. Select "{{console:discord.index.save}}".

Verification: the enabled post types have destinations configured in Channels. A go-live post is edited when the stream ends; confirm an actual wanted event reaches its destination.

Ask the user to confirm the observed result before continuing.

<a id="step-8"></a>
## Step 8: Configure tickets

1. Open "{{console:discord.nav.tickets}}" at `/discord/<guildId>/tickets` and enable "{{console:discord.tickets.label}}" if wanted.
2. Choose "{{console:discord.tickets.channelLabel}}" and "{{console:discord.tickets.categoryLabel}}".
3. Set "{{console:discord.tickets.staffRolesLabel}}" and "{{console:discord.tickets.openLimitLabel}}". Leaving every ticket-staff role unticked uses the configured staff roles.
4. Review "{{console:discord.tickets.archiveLabel}}", "{{console:discord.tickets.logLabel}}", and "{{console:discord.tickets.transcriptLabel}}". An unset archive deletes closed ticket channels; the ticket log falls back to staff logs.
5. Set panel title, description, button label, and colour as needed. Select "{{console:discord.index.save}}" before "{{console:discord.tickets.repostCta}}".

Reposting removes the old panel and posts a fresh one. Verification: the panel appears in the chosen channel, and an eligible member can open a private ticket that the chosen staff can access. Follow [ticket panel errors](../errors/discord.md#repost-failed) if it fails.

Ask the user to confirm the observed result before continuing.

<a id="step-9"></a>
## Step 9: Configure community and verify operation

1. Open "{{console:discord.nav.community}}" at `/discord/<guildId>/community`.
2. Review "{{console:discord.community.welcomeLabel}}", "{{console:discord.community.goodbyeLabel}}", "{{console:discord.community.voiceLabel}}", "{{console:discord.community.logsLabel}}", "{{console:discord.community.levelsLabel}}", "{{console:discord.community.linkGuardLabel}}", and "{{console:discord.community.subscribersLabel}}".
3. Enable only the intended features and select "{{console:discord.index.save}}".
4. Return to the Discord hub and confirm "{{console:discord.index.masterLabel}}" is on. Review the selected server's "{{console:discord.status.title}}" status and any setup warnings.

Welcome embeds use the server welcome channel. Goodbye and link guard are off by default. Link guard targets members with no role; staff logs also require their Community switch. Crumb ranks award 15 chat crumbs with a 60-second cooldown. Temporary rooms are deleted when empty.
Verification: test only enabled, configured features. A member can use `/rank` or `/daily` while ranks are on, or join the voice hub when voice is on. Check [gateway notices](../errors/discord.md#gateway-reconnecting) if the bot remains offline.

Ask the user to confirm the observed result before continuing.

<related>

- [Discord feature](../features/discord.md).
- [Discord errors](../errors/discord.md).
- [Getting started](getting-started.md).
- [Human Discord guide](https://itsbagelbot.com/guides/discord-setup/).

</related>

</page>
