---
title: Getting started with ItsBagelBot: setup reference for AI assistants
description: Twitch sign-in, onboarding choices, connection checks, and a first command or module.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/guides/getting-started/
---

[Human guide](https://itsbagelbot.com/guides/getting-started/).

Sign in with your Twitch channel account, finish onboarding, and check the bot connection.
Then create a command or enable the module you need.

<page type="setup">

<audience>

Assistants helping a channel owner start using ItsBagelBot or resume unfinished onboarding.

</audience>

<assistant_rules>

- Read the relevant section before guiding the user.
- Present one step at a time. Wait for the user to confirm or describe the result before continuing.
- {{console:setupAi.guideControls}}
- Follow only the fields, paths, and URLs documented on this page. If a setting or screen is not covered, say so.
- Do not request, accept, or repeat secrets, API keys, tokens, or passwords. Direct the user to paste credentials only into the designated dashboard or provider field. If a secret is pasted into chat, instruct the user to revoke or rotate it and enter only the replacement in the dashboard.
- If an error occurs, match it to the corresponding error reference page and follow its remediation steps.
- {{console:setupAi.guideLanguage}}
- If two attempts fail, direct the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Dashboard: https://dashboard.itsbagelbot.com/. Login: https://dashboard.itsbagelbot.com/login.
- Sign in with the channel owner's Twitch account. A delegate needs the owner's shared-dashboard invitation.
- OAuth permissions, bot moderation in chat, and individual feature switches are separate requirements.
- Connection controls are owner-only. Shared-dashboard access cannot change the connection.
- Onboarding offers a fresh setup, integration with an existing setup, quiet setup, or import.
- Quiet onboarding turns off shipped features and built-ins; custom commands and saved settings remain available.
- Finished features are {{website:pricingTiers.free}}. AutoMod and Discord require {{website:pricingTiers.premium}} during beta.
- Spotify and Govee have separate provider setup flows. Enabling their module alone does not finish that setup.

</facts>

<a id="step-1"></a>
## Step 1: Sign in with Twitch

1. Open https://dashboard.itsbagelbot.com/login.
2. Select "{{console:login.cta}}" and sign in with your own channel account.
3. Approve the requested access on Twitch's permission screen.

Verification: the dashboard or onboarding opens. If sign-in returns a notice, use [connection errors](../errors/connection.md#login).
Ask the user to confirm which screen opened before continuing.

<a id="step-2"></a>
## Step 2: Complete onboarding

1. If onboarding opens, use "{{console:onboarding.heroCta}}" and review the agreements shown.
2. At "{{console:onboarding.step1Title}}", type `/mod ItsBagelBot` in your own Twitch chat when moderation access is needed.
3. Choose a console language and the appropriate starting setup.
4. Use "{{console:onboarding.finishCta}}" when the flow is ready to finish.

The setup options are "{{console:onboarding.choiceNewTitle}}", "{{console:onboarding.choiceIntegrateTitle}}", "{{console:onboarding.choiceQuietTitle}}", and "{{console:onboarding.choiceImportTitle}}".
The import option opens a separate review flow. Use [import setup](import.md) for it.
Moderating the bot helps it answer in follower-only, subscriber-only, or slow-mode chat.

Verification: onboarding completes and the dashboard opens. If "{{console:onboarding.saveError}}" appears, inspect which choices saved before retrying.
Ask the user to confirm the starting setup and result before continuing.

<a id="step-3"></a>
## Step 3: Check the connection

1. Open the [dashboard overview](https://dashboard.itsbagelbot.com/) and inspect "{{console:overview.index.statusHeading}}".
2. If the bot is disabled, the channel owner selects "{{console:overview.index.enable}}".
3. If authorization needs refreshing, use "{{console:settings.reconnectTwitch}}" under [{{console:nav.settings}}](https://dashboard.itsbagelbot.com/settings).

Verification: "{{console:overview.index.onlineInChat}}" indicates healthy chat access. "{{console:overview.lifecycle.dormant}}" is also a valid idle state; enabled feature use wakes it.
Use [connection errors](../errors/connection.md) for a ban, timeout, or failed access check.
Ask the user to confirm the status before continuing.

<a id="step-4"></a>
## Step 4: Choose and test a feature

1. For a custom response, open [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands) and follow [commands setup](commands.md).
2. For a module, open [{{console:nav.modules}}](https://dashboard.itsbagelbot.com/modules), choose the feature, and read its reference.
3. Complete provider prerequisites and enable the feature before testing.

Verification: run an enabled command from an allowed account and observe the reply.
A live-only feature needs a live stream, or its configured offline option.
Ask the user to confirm the result before supplying more configuration steps.

<related>

- [Product](../product.md).
- [Commands setup](commands.md).
- [Import setup](import.md).
- [Spotify setup](../spotify/setup.md).
- [Govee setup](../govee/setup.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/getting-started/).

</related>

</page>
