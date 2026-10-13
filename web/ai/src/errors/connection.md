---
title: Bot connection troubleshooting: reference for AI assistants
description: Match Twitch sign-in, authorization, bot bans, timeouts, dormant state, and silent commands to verified recovery controls.
updated: 2026-10-13
kind: errors
human: https://itsbagelbot.com/guides/setup-errors/
---

[Human guide](https://itsbagelbot.com/guides/setup-errors/).

Check the dashboard connection state before changing a feature. Twitch authorization, disabled operation, chat bans, timeouts, and dormant state need different responses.
For silent commands, also check command access, enable state, cooldowns, and stream requirements.

<page type="errors">

<audience>

Assistants helping a streamer identify a dashboard connection notice or explain missing chat replies.

</audience>

<assistant_rules>

- Read the relevant section before guiding the user.
- Explain matching causes directly. If guiding active remediation, present one step at a time and wait for user confirmation before continuing.
- {{console:setupAi.guideControls}}
- {{console:setupAi.guideLanguage}}
- Do not request, accept, or output secrets, API keys, tokens, or passwords. Direct the user to enter them only in provider or dashboard fields. If a secret is pasted into chat, instruct the user to revoke or rotate it and enter only the replacement in the dashboard.
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Connection status is shown in "{{console:overview.index.statusHeading}}" on https://dashboard.itsbagelbot.com/.
- Only the channel owner can enable, disconnect, restart, or retry the bot connection. Delegates can inspect it.
- "{{console:overview.index.onlineInChat}}" represents healthy chat access.
- "{{console:overview.lifecycle.dormant}}" preserves configuration and wakes when an enabled feature is used.
- Twitch chat bans/timeouts and revoked OAuth access are separate conditions. Unbanning does not restore missing authorization.
- "{{console:overview.index.unavailable}}" means status could not be established, not proof that the bot is banned.
- Module enable state and command permissions are checked separately from connection health.

</facts>

<a id="login"></a>
## Twitch sign-in did not complete

Match the login notice:

- "{{console:login.noticeDeclined}}": restart using "{{console:login.cta}}" and approve Twitch's permission screen.
- "{{console:login.noticeState}}": start a fresh login from the current tab.
- "{{console:login.noticeOauth}}": wait a moment, then start sign-in again.
- "{{console:login.noticeScope}}": restart sign-in and approve the requested access.
- "{{console:login.noticeBot}}": sign out of the bot account on Twitch and use the broadcaster's account.
- "{{console:login.noticeRetry}}": retry sign-in; the server did not finish saving it.

Verification: the dashboard opens under the intended channel identity.

<a id="auth"></a>
## Authorization missing or revoked

Messages include "{{console:overview.index.issueNoAuth}}", "{{console:overview.index.reconnectNeeded}}", and "{{console:overview.index.twitchAccessLost}}".
A scope gap can show "{{console:overview.index.issueTwitchScope}}".

1. The owner opens [{{console:nav.settings}}](https://dashboard.itsbagelbot.com/settings).
2. Select "{{console:settings.reconnectTwitch}}" and approve Twitch access again.
3. Return to the overview and inspect status.

Verification: access warnings clear and chat status becomes healthy or a valid idle state.
A password change or disconnecting the app on Twitch can revoke authorization.

<a id="disabled"></a>
## Bot disabled

Message: "{{console:overview.index.statusPausedDetail}}". Lifecycle label: "{{console:overview.lifecycle.disabledByYou}}".

The owner selects "{{console:overview.index.enable}}" on the overview.
If the disabled state retains a chat ban, first unban the bot and use "{{console:overview.lifecycle.enableAndVerify}}".
Verification: the disabled state clears. Follow any separate authorization or ban warning that remains.

<a id="banned"></a>
## Bot banned from chat

Message: "{{console:overview.index.botBanned}}".

1. The broadcaster or a moderator types `/unban ItsBagelBot` in the channel's Twitch chat.
2. If the page offers "{{console:overview.lifecycle.verifyUnban}}", the owner selects it after unbanning.
3. If "{{console:overview.lifecycle.secondaryGrantMissing}}" appears, reconnect Twitch as well.

Healthy control listeners can restore operation automatically after an unban. Otherwise the page offers a verification action.
If the bot was disabled too, use "{{console:overview.lifecycle.enableAndVerify}}" after unbanning.
Verification: the ban state clears and access is verified.

<a id="timeout"></a>
## Bot timed out

Label: "{{console:overview.lifecycle.timedOut}}". Message: "{{console:overview.lifecycle.issueTimedOut}}".

Wait for the timeout to end. If the page offers "{{console:overview.lifecycle.retryConnection}}", use it after the restriction ends.
Verification: normal work resumes after Twitch access is verified.

<a id="dormant"></a>
## Bot dormant

Label: "{{console:overview.lifecycle.dormant}}". Message: "{{console:overview.lifecycle.issueDormant}}".

Use an enabled feature from an allowed account to wake the bot.
Verification: the feature responds and normal activity resumes. Dormant state itself does not require deleting configuration or reconnecting OAuth.

<a id="recovering"></a>
## Recovery or connection check failed

Recovery label: "{{console:overview.lifecycle.recovering}}". Explanation: "{{console:overview.lifecycle.issueRecovering}}".
Failed check: "{{console:overview.lifecycle.connectionCheckFailed}}" with "{{console:overview.lifecycle.issueConnectionCheckFailed}}".

Allow an in-progress recovery to finish. When the underlying issue is resolved, use "{{console:overview.lifecycle.retryConnection}}" if offered.
For "{{console:overview.index.unavailable}}" or "{{console:overview.index.connectedIdle}}", use "{{console:overview.index.retry}}" if offered.
Verification: the status resolves to a known state; follow that state's section if it remains blocked.

<a id="silent"></a>
## Connected but commands are silent

Messages can include "{{console:overview.index.issueAllDisabled}}" or "{{console:overview.index.issueNoCommands}}".

1. Inspect [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands) for the intended name and active state.
2. Check access, a user-ID restriction, shared and per-viewer cooldowns, and live-only settings.
3. For a module command, confirm that module and its relevant command/reply are enabled.
4. Check provider prerequisites for Spotify, Govee, or a game-stat lookup.

Verification: an allowed account receives the expected reply when the feature's requirements are met.
A quiet onboarding choice can disable shipped commands and modules intentionally.
For chat restrictions such as follower-only or slow mode, moderate the bot with `/mod ItsBagelBot` as described in onboarding.
Ask the user for the exact command and visible notice, never for credentials.

<related>

- [Getting started](../setup/getting-started.md).
- [Commands reference](../features/commands.md).
- [Spotify errors](../spotify/errors.md).
- [Govee errors](../govee/errors.md).
- [Human setup-errors guide](https://itsbagelbot.com/guides/setup-errors/).
- [Support](https://itsbagelbot.com/contact/) and [community](https://discord.gg/SZ2remwSDv).

</related>

</page>
