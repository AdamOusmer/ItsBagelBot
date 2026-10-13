---
title: Create a custom command: setup reference for AI assistants
description: Create, configure, save, and test a custom Twitch chat response using exact dashboard labels.
updated: 2026-10-13
kind: setup
human: https://itsbagelbot.com/guides/commands/
---

[Human guide](https://itsbagelbot.com/guides/commands/).

Create a command in the dashboard, write its response, choose who can run it, and save.
Test it in Twitch chat with the configured name and an allowed account.

<page type="setup">

<audience>

Assistants guiding a broadcaster or a delegate with command-management access through a custom command.

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

- Dashboard: [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands).
- A new custom command defaults to active, everyone, shared cooldown 0, per-viewer cooldown 0, and allowed offline.
- Command names and aliases are at most 64 characters, without whitespace. The editor stores them without `!`.
- Responses allow 5 non-empty lines, at most 500 characters each. Every line sends its own message.
- Cooldowns range from 0 to 86400 seconds. Built-in and existing names or aliases cannot be reused.
- Variables use single braces, for example `{user}`, `{args}`, and `{touser|everyone}`.
- Module variables require the referenced module; preview uses sample data and does not enable the module.
- "{{console:commandEditor.restrictUser}}" accepts a numeric Twitch ID, not a username.

</facts>

<a id="step-1"></a>
## Step 1: Open the editor

1. Open https://dashboard.itsbagelbot.com/commands.
2. Select "{{console:commands.newCommand}}".
3. Enter a unique name in "{{console:commandEditor.name}}", such as `lurk` if unused.

Verification: the editor is open for the intended command. If the name already exists, edit that command or choose another name.
Ask the user to confirm the name before continuing.

<a id="step-2"></a>
## Step 2: Write the response

1. Enter the intended chat text in "{{console:commandEditor.response}}".
2. Use "{{console:commandEditor.insertVariable}}" for dynamic values.
3. Add aliases with "{{console:commandEditor.altNames}}" if desired.

A starter example is "{{console:commands.starters.lurk.response}}".
Use "{{console:commandEditor.addLine}}" for a separate chat message, within the five-line limit.
Verification: the preview shows the intended response with sample values. Confirm the text before continuing.

<a id="step-3"></a>
## Step 3: Choose access and cooldowns

1. Choose "{{console:commandEditor.access}}" for the intended audience.
2. Set "{{console:commandEditor.cooldownS}}" and "{{console:commandEditor.userCooldownS}}" as needed.
3. Confirm "{{console:commandEditor.active}}" is on.
4. Use "{{console:commandEditor.onlyWhileLive}}" when the response should require a live stream.

"{{console:commandEditor.bumpCounter}}" can increment an existing counter on successful execution.
Verification: the settings match the intended permissions and stream state. Ask the user to confirm before saving.

<a id="step-4"></a>
## Step 4: Save and test

1. Select "{{console:commandEditor.create}}" for a new command, or "{{console:commandEditor.saveChanges}}" for an edit.
2. Confirm the saved command is listed.
3. Type its trigger in the channel's Twitch chat, for example `!lurk` for the name above.

Verification: a new command shows "{{console:commands.toastCreated}}", with its name substituted, and chat receives the configured reply.
If "{{console:commands.toastSaveFailed}}" appears, inspect the saved state before retrying.
If chat is silent, check bot connection, command active state, access, both cooldowns, and live-only settings.
Use [connection errors](../errors/connection.md#silent) if those checks do not explain it.
Ask the user to confirm the observed reply.

<related>

- [Commands reference](../features/commands.md).
- [Getting started](getting-started.md).
- [Connection errors](../errors/connection.md).
- [Human commands guide](https://itsbagelbot.com/guides/commands/).
- [Human variables reference](https://itsbagelbot.com/guides/variables/).

</related>

</page>
