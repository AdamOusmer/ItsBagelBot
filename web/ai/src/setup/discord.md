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
## Step 4: Preview and apply the optional server template

The bot install only adopts channels and roles the server already has; it creates nothing. The server template is optional.

1. Open "{{console:discord.nav.settings}}" at `/discord/<guildId>/settings`.
2. In "{{console:discord.settings.templateTitle}}", select "{{console:discord.settings.previewCta}}". Nothing in the server changes at this point.
3. Review "{{console:discord.settings.createHeading}}" and "{{console:discord.settings.reuseHeading}}". Existing channels and roles are reused, and pinned roles are marked "{{console:discord.settings.pinnedTag}}".
4. To add the missing roles and channels, select "{{console:discord.settings.applyCta}}" and confirm the "{{console:discord.settings.applyTitle}}" dialog. To skip the template, leave the server as it is and choose destinations on the module pages.

The template adds missing resources only; it does not delete or rename existing channels or roles. If you want it to reuse existing roles, configure and pin them in the next step, then preview again.
Verification: inspect the server's actual channels and roles. If "{{console:discord.toast.refused}}" appears, choose remaining destinations on the module pages.

Ask the user to confirm the observed result before continuing.

<a id="step-5"></a>
## Step 5: Map roles

1. Open the roles module at `/discord/<guildId>/modules/roles`.
2. Choose "{{console:discord.modules.roles.fields.ownerRoleId.label}}", "{{console:discord.modules.roles.fields.leadModRoleId.label}}", and "{{console:discord.modules.roles.fields.modsRoleId.label}}" for the intended staff.
3. Choose "{{console:discord.modules.roles.fields.subscriberRoleId.label}}", "{{console:discord.modules.roles.fields.vipRoleId.label}}", and "{{console:discord.modules.roles.fields.regularsRoleId.label}}" as needed. The member role for joiners is set in the auto role module in step 9.
4. Use "{{console:discord.modules.roles.fields.pinnedRoles.label}}" to keep existing roles if the server template should reuse them.
5. Select "{{console:discord.index.save}}" and verify the saved mappings.

The bot creates tier roles but you assign them yourself. Discord's separate Twitch integration can synchronize subscribers only for an Affiliate or Partner account; it is configured in Discord.
Verification: each slot points to the intended existing role below the bot's role. A missing pinned role uses [this warning](../errors/discord.md#dropped-pins).

Ask the user to confirm the observed result before continuing.

<a id="step-6"></a>
## Step 6: Choose channel destinations

1. Open the live alerts module at `/discord/<guildId>/modules/live` and the clips module at `/discord/<guildId>/modules/clips`. Choose "{{console:discord.modules.live.fields.liveChannelId.label}}" and "{{console:discord.modules.clips.fields.clipsChannelId.label}}" for wanted posts.
2. Open the welcome module at `/discord/<guildId>/modules/welcome` and choose "{{console:discord.modules.welcome.fields.welcomeChannelId.label}}".
3. Open the server logs module at `/discord/<guildId>/modules/logs`. Choose "{{console:discord.modules.logs.fields.logChannelId.label}}" and any individual log destinations needed.
4. For temporary voice, open `/discord/<guildId>/modules/voice` and set "{{console:discord.modules.voice.fields.voiceHubId.label}}" and "{{console:discord.modules.voice.fields.voiceCategoryId.label}}" together. Choose room naming, privacy, and user limit as needed; 0 is unlimited and the maximum is 99.
5. Open `/discord/<guildId>/modules/subscribers` if subscriber and VIP rooms are used. Select "{{console:discord.index.save}}" on each module page you change.

Verification: saved destinations match the intended server channels and remain accessible to the bot. Empty destinations skip posts. Follow [blocked destination errors](../errors/discord.md#cannot-post) if the bot cannot post.

Ask the user to confirm the observed result before continuing.

<a id="step-7"></a>
## Step 7: Configure announcements

1. Open the live alerts module at `/discord/<guildId>/modules/live` and confirm its switch ("{{console:discord.module.statusTitle}}") is on. Do the same for the clips module.
2. Review "{{console:discord.modules.live.fields.categoryAllow.label}}" and "{{console:discord.modules.live.fields.categoryDeny.label}}". An empty allow list allows every category; a denied category wins.
3. Select "{{console:discord.index.save}}".

Verification: the enabled post types have destinations configured in their module pages. A go-live post is edited when the stream ends; confirm an actual wanted event reaches its destination.

Ask the user to confirm the observed result before continuing.

<a id="step-8"></a>
## Step 8: Configure tickets

1. Open "{{console:discord.nav.tickets}}" at `/discord/<guildId>/tickets` and turn the module on with its switch ("{{console:discord.module.statusTitle}}") if wanted. The switch saves immediately.
2. Choose "{{console:discord.modules.tickets.fields.ticketChannelId.label}}" and "{{console:discord.modules.tickets.fields.ticketCategoryId.label}}".
3. Set "{{console:discord.modules.tickets.fields.ticketStaffRoleIds.label}}" and "{{console:discord.modules.tickets.fields.ticketOpenLimit.label}}". Leaving every ticket-staff role unticked uses the configured staff roles.
4. Review "{{console:discord.modules.tickets.fields.ticketArchiveCategoryId.label}}", "{{console:discord.modules.tickets.fields.ticketLogChannelId.label}}", and "{{console:discord.modules.tickets.fields.ticketTranscriptEnabled.label}}". An unset archive deletes closed ticket channels; the ticket log falls back to staff logs.
5. Set panel title, description, button label, and colour as needed. Select "{{console:discord.index.save}}" before "{{console:discord.tickets.repostCta}}".

Reposting removes the old panel and posts a fresh one. Verification: the panel appears in the chosen channel, and an eligible member can open a private ticket that the chosen staff can access. Follow [ticket panel errors](../errors/discord.md#repost-failed) if it fails.

Ask the user to confirm the observed result before continuing.

<a id="step-9"></a>
## Step 9: Configure remaining modules and verify operation

1. Open the server page at `/discord/<guildId>` and review the "{{console:discord.overview.modulesTitle}}" list. Use "{{console:discord.overview.configure}}" to open a module.
2. Review the welcome, goodbye ("{{console:discord.modules.goodbye.label}}"), auto role ("{{console:discord.modules.autorole.label}}"), voice ("{{console:discord.modules.voice.label}}"), logs ("{{console:discord.modules.logs.label}}"), levels ("{{console:discord.modules.levels.label}}"), link guard ("{{console:discord.modules.linkguard.label}}"), and subscriber ("{{console:discord.modules.subscribers.label}}") modules.
3. Turn on only the intended modules with each module's switch and save any settings you change with "{{console:discord.index.save}}".
4. Return to the Discord hub and confirm "{{console:discord.index.masterLabel}}" is on. Review the selected server's "{{console:discord.status.title}}" status and any setup warnings.

Welcome embeds use the server welcome channel. Goodbye, subscriber rooms and link guard are off by default. Link guard targets members with no role; each log type also has its own switch in the server logs module. Crumb ranks award 15 chat crumbs with a 60-second cooldown. Temporary rooms are deleted when empty.
Verification: test only enabled, configured features. A member can use `/rank` or `/daily` while ranks are on, or join the voice hub when voice is on. Check [gateway notices](../errors/discord.md#gateway-reconnecting) if the bot remains offline.

Ask the user to confirm the observed result before continuing.

<related>

- [Discord feature](../features/discord.md).
- [Discord errors](../errors/discord.md).
- [Getting started](getting-started.md).
- [Human Discord guide](https://itsbagelbot.com/guides/discord-setup/).

</related>

</page>
