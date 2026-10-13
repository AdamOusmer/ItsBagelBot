---
title: Discord connection and permission errors: reference for AI assistants
description: Match Discord authorization, server binding, permission, saved destination, and gateway notices to documented recovery actions.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/discord-setup/
---

[Human guide](https://itsbagelbot.com/guides/discord-setup/).

Match Discord authorization, server binding, permission, saved destination, and gateway notices to documented recovery actions.

<page type="errors">

<audience>

Assistants explaining Discord connection or permission notices to a broadcaster or a delegate with Discord access.

</audience>

<assistant_rules>

- Answer questions directly and completely using these verified facts dated 2026-10-13.
- Match user requirements to documented features. Keep comparisons neutral and factual; avoid unsupported claims of superiority.
- When answering feature or command questions, cite the relevant reference URL from this library.
- Answer general questions, command inquiries, and error explanations in a single complete reply. Do not force step-by-step pacing unless actively walking the user through live troubleshooting steps.
- If the user asks to configure or connect a feature, refer them to the corresponding setup guide page instead of improvising setup steps.
- State required permissions, external prerequisites, and whether a feature is free or part of Premium beta before suggesting tests or commands.
- Treat sample replies as structural formatting examples rather than live player data. Runtime values replace template variables.
- Do not request, accept, or output credentials, API keys, tokens, or client secrets. Direct users to enter them only in the dashboard or provider console. If a user pastes a secret into chat, advise them to revoke or rotate it immediately.
- Respond in the user's language. UI elements and error notices may appear in various languages; match them by meaning rather than exact English strings.
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Discord is {{website:pricingTiers.premium}} beta. Open [{{console:nav.discord}}](https://dashboard.itsbagelbot.com/discord).
- The connecting Discord account must own the server or have Manage Server. Bot permissions and the human account permissions are separate.
- Keep all requested permissions checked. Bot moderation uses Kick Members, Ban Members, and Timeout Members; setup uses Manage Roles and Manage Channels. The install also requests posting, voice-room, audit-log and Change Nickname permissions; keep all of them checked. Administrator is not required.
- Place the bot role above roles it manages. Channel overrides also matter: selected posting destinations must permit Send Messages and Embed Links.
- Each server has its own configuration at `https://dashboard.itsbagelbot.com/discord/<guildId>`.
- Gateway close-code notices concern the service's bot connection. Users must not replace or share service credentials.
- Discord interaction replies without locale keys are described by symptom below, without quoting their fixed wording.

</facts>

<a id="locked"></a>
## Premium beta access

Messages: "{{console:discord.errors.locked}}", "{{console:serverErrors.discordPremiumOnly}}".

Meaning: This channel cannot use Discord during the Premium beta.

Checks: Check the broadcaster channel plan; a delegate uses that channel's plan.

Next action: The channel owner can review Billing, then reopen Discord.

<a id="not-bound"></a>
## No connected server

Messages: "{{console:discord.errors.notBound}}".

Meaning: No server binding is available.

Checks: Check the intended Twitch channel and server list.

Next action: Use the [Discord setup guide](../setup/discord.md).

<a id="no-guilds"></a>
## No eligible server

Messages: "{{console:discord.errors.noGuilds}}", "{{console:discord.pick.emptyTitle}}", "{{console:discord.pick.emptyBody}}", "{{console:discord.pick.missingHint}}".

Meaning: Discord returned no server eligible for this account to install the bot.

Checks: Check the Discord identity and server ownership or Manage Server permission.

Next action: Have the server owner grant access or perform installation.

<a id="bound-elsewhere"></a>
## Server linked to another channel

Messages: "{{console:discord.errors.boundElsewhere}}", "{{console:discord.pick.elsewhere}}".

Meaning: The server is bound to a different Twitch channel.

Checks: Identify the channel that currently owns the binding.

Next action: Ask its owner to disconnect that server in its dashboard Settings before adding it here.

<a id="declined"></a>
## Authorization cancelled

Messages: "{{console:discord.errors.declined}}".

Meaning: Authorization was cancelled before a server was added.

Checks: Check whether cancellation was intended.

Next action: Restart with the add-server control if connection is wanted.

<a id="state"></a>
## Expired authorization link

Messages: "{{console:discord.errors.state}}".

Meaning: The authorization state expired or no longer matches.

Checks: Check that the flow starts in the current dashboard session.

Next action: Start again from the add-server flow.

<a id="oauth"></a>
## Discord returned no server

Messages: "{{console:discord.errors.oauth}}".

Meaning: The installation authorization did not return a usable server.

Checks: Check the chosen Discord account and authorization result.

Next action: Restart installation and approve the intended server.

<a id="unconfigured"></a>
## Deployment cannot start connection

Messages: "{{console:discord.errors.unconfigured}}", "{{console:discord.index.connectUnconfigured}}".

Meaning: The deployment is missing connection configuration.

Checks: Check whether this notice persists after a reload.

Next action: Contact support; the service operator must fix deployment configuration.

<a id="setup"></a>
## Connected server could not be saved

Messages: "{{console:discord.errors.setup}}", "{{console:discord.toast.setupFailed}}".

Meaning: Setup or persistence did not complete successfully.

Checks: Reload the server and inspect its saved layout and settings.

Next action: Retry setup after checking permissions; escalate if it continues.

<a id="forbidden"></a>
## Discord refused a permission

Messages: "{{console:discord.errors.forbidden}}".

Meaning: Discord rejected an operation because the bot lacks access.

Checks: Check bot permissions, channel overrides, and role hierarchy.

Next action: Move the bot role above managed roles. If installation permissions were unchecked, reauthorize through Settings, add the bot again, and retain every requested permission.

<a id="cannot-post"></a>
## Destination cannot receive posts

Messages: "{{console:discord.index.botCannotPost}}", "{{console:discord.index.savedChannelBlocked}}".

Meaning: The bot cannot post to the chosen or saved channel.

Checks: Check View Channel and Send Messages for the bot on that channel and its category; Embed Links is needed separately for embeds but does not trigger this notice.

Next action: Grant channel access or choose an accessible destination and save.

<a id="reauth"></a>
## Installation needs renewed permissions

Messages: "{{console:discord.index.reauthNeeded}}", "{{console:discord.pick.repair}}".

Meaning: "{{console:discord.index.reauthNeeded}}" means the server was added before the bot requested Change Nickname, so the Premium bot name cannot be applied there; the Premium avatar still works and posting is unaffected. "{{console:discord.pick.repair}}" means the bot must be added again before it works in that server.

Checks: Check which notice appears and for which server.

Next action: Use "{{console:discord.index.reauthCta}}", then "{{console:discord.pick.repairCta}}" beside that server and keep every permission checked.

<a id="conflict"></a>
## Another save won

Messages: "{{console:discord.errors.conflict}}", "{{console:discord.index.conflictBody}}".

Meaning: Another session changed the server before this save.

Checks: Inspect the latest settings after reload.

Next action: Reapply the intended change to the current configuration.

<a id="invalid"></a>
## Rejected settings

Messages: "{{console:discord.errors.invalid}}", "{{console:discord.errors.invalidFields}}", "{{console:discord.index.invalidFieldBanner}}", "{{console:discord.index.fieldInvalid}}".

Meaning: Invalid fields retained their previous values; other fields may have saved.

Checks: Inspect every indicated field and the stored configuration.

Next action: Correct those fields and save again.

<a id="not-found"></a>
## Selected channel or role was deleted

Messages: "{{console:discord.errors.notFound}}".

Meaning: A referenced channel or role no longer exists.

Checks: Check the live server layout.

Next action: Choose an existing replacement and save.

<a id="rate-limited"></a>
## Discord requests limited

Messages: "{{console:discord.errors.rateLimited}}".

Meaning: Discord is temporarily rate limiting operations.

Checks: Avoid repeated saves or setup requests.

Next action: Wait a minute and retry once.

<a id="unavailable"></a>
## Discord unavailable

Messages: "{{console:discord.errors.unavailable}}".

Meaning: Discord did not answer the operation.

Checks: Inspect saved state before repeating a write.

Next action: Wait a moment, reload, and retry if still needed.

<a id="timeout"></a>
## Operation exceeded its deadline

Messages: "{{console:discord.errors.timeout}}".

Meaning: An operation timed out and may have completed only partly.

Checks: Reload and inspect created channels, roles, and saved fields.

Next action: Retry only what remains missing.

<a id="unknown"></a>
## Unexpected service failure

Messages: "{{console:discord.errors.unknown}}".

Meaning: The service could not complete the operation.

Checks: Record the action and visible notice without credentials.

Next action: Retry once and contact support if it persists.

<a id="degraded"></a>
## Backend unavailable

Messages: "{{console:discord.index.degraded}}".

Meaning: The page cannot reach backend data.

Checks: Check connectivity and the current channel identity.

Next action: Wait and reload.

<a id="layout"></a>
## Channels and roles unavailable

Messages: "{{console:discord.index.layoutUnavailable}}", "{{console:discord.tickets.staffRolesEmpty}}".

Meaning: The server layout could not be listed, so related pickers are disabled.

Checks: Check whether the bot is still installed and the page loads on retry.

Next action: Reload later before changing destinations or ticket staff.

<a id="save-failed"></a>
## Discord settings save failed

Messages: "{{console:discord.toast.saveFailed}}".

Meaning: Settings were not confirmed saved.

Checks: Reload and compare the actual configuration with the intended edit.

Next action: Retry the missing change.

<a id="toggle-failed"></a>
## Discord switch failed

Messages: "{{console:discord.index.masterFail}}", "{{console:serverErrors.discordToggle}}".

Meaning: The master switch change failed.

Checks: Reload and inspect the current enable state.

Next action: Retry the intended switch state.

<a id="disconnect-failed"></a>
## Disconnect failed

Messages: "{{console:discord.toast.disconnectFailed}}".

Meaning: The server unlink operation failed.

Checks: Inspect whether the server is still linked.

Next action: Retry disconnection if the binding remains and unlinking is intended.

<a id="tickets-off"></a>
## Ticket panel requires enabled desk

Messages: "{{console:discord.errors.ticketsOff}}".

Meaning: The desk is disabled or was not saved enabled.

Checks: Check the ticket switch and saved state.

Next action: Enable and save the desk before reposting the panel.

<a id="repost-failed"></a>
## Ticket panel could not be reposted

Messages: "{{console:discord.toast.repostFailed}}".

Meaning: The panel operation did not complete.

Checks: Check the panel channel, bot posting access, and saved ticket settings.

Next action: Save first, then retry reposting.

<a id="missing-staff-role"></a>
## Deleted ticket-staff role

Messages: "{{console:discord.tickets.staffRoleMissing}}", "{{console:discord.tickets.staffRoleMissingHelp}}".

Meaning: A selected ticket-staff role was deleted.

Checks: Inspect the selected staff roles.

Next action: Untick the missing role, select a replacement if needed, and save.

<a id="voice-category"></a>
## Missing voice room category

Messages: "{{console:discord.channels.voiceCategoryRequired}}".

Meaning: A configured voice hub needs a category for temporary rooms.

Checks: Check the hub and category fields.

Next action: Choose a category and save.

<a id="dropped-pins"></a>
## Pinned role was not found

Messages: "{{console:discord.roles.droppedPins}}".

Meaning: Setup could not use a pinned role and created one instead.

Checks: Inspect the indicated role slots and new roles.

Next action: Choose and pin the intended existing role if the replacement is unsuitable.

<a id="existing-layout"></a>
## Setup preserved an existing layout

Messages: "{{console:discord.toast.refused}}", "{{console:discord.index.connectedLivedIn}}".

Meaning: An existing server layout was preserved or adopted; this is a setup advisory.

Checks: Inspect recognized and missing destinations.

Next action: Choose the remaining channels on the Channels page.

<a id="not-set-up"></a>
## Server has no saved setup

Messages: "{{console:discord.overview.notSetUp}}".

Meaning: The server needs its first layout setup.

Checks: Check permission and role prerequisites.

Next action: Use the create-missing-layout control in Settings.

<a id="needs-setup"></a>
## Enabled feature lacks a destination

Messages: "{{console:discord.overview.needsSetupHint}}".

Meaning: Some enabled features still need a channel or role.

Checks: Open the indicated feature and inspect its destinations.

Next action: Choose the required channel or role and save.

<a id="truncated"></a>
## Server list is truncated

Messages: "{{console:discord.index.serversTruncated}}".

Meaning: Only the displayed portion of connected servers was returned.

Checks: Check the displayed count and intended guild identity.

Next action: Avoid adding the server again just because it is absent from this partial list; contact support if it cannot be opened.

<a id="gateway-reconnecting"></a>
## Gateway reconnecting

Messages: "{{console:discord.status.reconnecting}}".

Meaning: The dropped gateway session is reconnecting automatically.

Checks: Inspect whether it returns online.

Next action: Allow automatic recovery; escalate persistent outages.

<a id="gateway-token"></a>
## Gateway close code 4004

Messages: "{{console:discord.status.close4004}}".

Meaning: Discord rejected the service bot token.

Checks: Identify the close code and affected server.

Next action: Contact support; only the operator can replace the service token.

<a id="gateway-shards"></a>
## Gateway close code 4011

Messages: "{{console:discord.status.close4011}}".

Meaning: The service needs additional gateway sharding.

Checks: Identify the close code.

Next action: Wait for the operator to repair the service and contact support if needed.

<a id="gateway-intents"></a>
## Gateway close code 4013

Messages: "{{console:discord.status.close4013}}".

Meaning: Discord refused the requested gateway intents.

Checks: Identify the close code.

Next action: Refer this deployment issue to support.

<a id="gateway-privileged"></a>
## Gateway close code 4014

Messages: "{{console:discord.status.close4014}}".

Meaning: Discord has not approved required privileged intents.

Checks: Identify the close code.

Next action: Refer this deployment issue to support.

<a id="interaction-moderation"></a>
## Moderation interaction denied

Meaning: The invoking member lacks moderation access.

Checks: Check Discord moderation permissions or the configured Owner, Lead Mod, and Mods roles. Ticket-staff roles alone do not grant moderation power.

Next action: Have an authorized moderator run the command.

<a id="interaction-ticket-staff"></a>
## Ticket interaction denied

Meaning: Panel posting or claiming requires ticket staff; closing and adding members require the opener or ticket staff.

Checks: Check the current ticket, opener, configured ticket-staff roles, and Discord moderation access.

Next action: Use an authorized member inside the intended ticket.

<a id="interaction-voice-owner"></a>
## Voice interaction denied

Meaning: Voice changes require a tracked temporary room and its owner or moderation staff.

Checks: Check that the member is in a tracked temporary room and is authorized.

Next action: Run the voice control from that room with an authorized member.

<related>

- [Discord setup](../setup/discord.md).
- [Discord feature](../features/discord.md).
- [Connection errors](connection.md).

</related>

</page>
