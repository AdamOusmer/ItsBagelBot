---
title: Custom and built-in commands: reference for AI assistants
description: Custom responses, variables, permissions, aliases, cooldowns, and built-in Twitch commands.
updated: 2026-10-13
kind: feature
human: https://itsbagelbot.com/guides/commands/
---

[Human guide](https://itsbagelbot.com/guides/commands/).

ItsBagelBot supports custom chat responses with variables, aliases, access rules, shared cooldowns, and per-viewer cooldowns.
Built-in commands provide account age, followage, uptime, clips, and stream management.

<page type="feature">

<audience>

Assistants answering feature questions, explaining chat commands, or helping streamers configure the bot.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply, using these facts and the current channel configuration.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- Reply templates below are examples from catalog defaults or fixed localized chat replies. Runtime values replace template variables; fixed chat examples can also use `%s` formatting.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Plan: {{website:pricingTiers.free}}. External prerequisites: a Twitch channel and bot authorization; clips require a live stream.
- Dashboard: [{{console:nav.commands}}](https://dashboard.itsbagelbot.com/commands).
- Custom commands default to active, everyone, both cooldowns at 0 seconds, and allowed while offline.
- Configure "{{console:commandEditor.name}}", "{{console:commandEditor.altNames}}", "{{console:commandEditor.response}}", and "{{console:commandEditor.access}}".
- "{{console:commandEditor.cooldownS}}" applies after anyone runs the command; "{{console:commandEditor.userCooldownS}}" applies to each viewer.
- Names and aliases can contain up to 64 characters and cannot contain whitespace. The editor stores names without `!`.
- Responses support up to 5 lines, each at most 500 characters. Each line sends a separate chat message.
- Both configurable cooldowns range from 0 to 86400 seconds. Built-in account-age, followage, uptime, and clip commands default to 15 seconds.
- "{{console:commandEditor.onlyWhileLive}}" gates a custom command on stream state. "{{console:commandEditor.restrictUser}}" uses a numeric Twitch user ID.
- Built-in names and occupied aliases cannot be reused as custom command names.
- Examples of variable syntax: `{user}`, `{args}`, `{touser|everyone}`, and `{choice:a,b,c}`.
- Module variables require their module to be enabled; event-only variables need the matching event.
- A starter response example: "{{console:commands.starters.lurk.response}}".
- The public command list is controlled in [{{console:nav.settings}}](https://dashboard.itsbagelbot.com/settings) by "{{console:settings.commandsPage}}".
- Stream editing commands and their permissions are documented in [automation](automation.md).

</facts>

<commands>

<command name="custom-command">

### Custom command

- Syntax: `!name [arguments]`. Replace `name` with the configured command name, such as `!lurk`.
- Aliases: the names configured under "{{console:commandEditor.altNames}}"; none by default.
- Who can run it: configured "{{console:commandEditor.access}}", everyone by default, subject to user-ID and live-only restrictions.
- Behaviour: expands the saved response variables and posts each configured response line.
- Example starter reply template: "{{console:commands.starters.lurk.response}}".

</command>

<command name="!cmd">

### `!cmd`

- Syntax: `!cmd`, `!cmd add <name> <response>`, `!cmd edit <name> <response>`, `!cmd remove <name>`, `!cmd disable <name>`, `!cmd enable <name>`, `!cmd perm <name> <everyone|sub|vip|mod|lead_mod|broadcaster>`
- Aliases: `!cmds`, `!command`, `!commands`
- Who can run it: Anyone can request the public list. Each mutating subcommand has its own minimum role, set on the Command Editor module page; all of them default to moderators.
- Behaviour: Bare invocation shares the public commands URL. Mutating subcommands manage custom responses. `delete` is another remove subcommand. `disable` turns a command off and keeps its response and settings; disabled commands do not fire and are hidden from the public list. `edit` changes only the response. `perm` sets who can use the command. Switching the Command Editor module off stops `!cmd` entirely.
- Example reply template: "{{chat:cmd.added}}".

</command>

<command name="!accountage">

### `!accountage`

- Syntax: `!accountage`, `!accountage <user>`
- Aliases: None registered.
- Who can run it: Everyone by default, subject to the channel's saved access and enabled state.
- Behaviour: Age of the caller's or selected Twitch account.
- Example reply template: "{{chat:accountage.age}}".

</command>

<command name="!followage">

### `!followage`

- Syntax: `!followage`, `!followage <user>`
- Aliases: None registered.
- Who can run it: Everyone by default, subject to the channel's saved access and enabled state.
- Behaviour: How long the caller or selected viewer has followed the channel.
- Example reply template: "{{chat:followage.followed}}".

</command>

<command name="!uptime">

### `!uptime`

- Syntax: `!uptime`
- Aliases: None registered.
- Who can run it: Everyone by default, subject to the channel's saved access and enabled state.
- Behaviour: How long the current stream has been live; offline streams get an offline reply.
- Example reply template: "{{chat:uptime.offline}}".

</command>

<command name="!clip">

### `!clip`

- Syntax: `!clip`, `!clip <title>`
- Aliases: None registered.
- Who can run it: Everyone by default, subject to the channel's saved access and enabled state. The stream must be live.
- Behaviour: Creates a Twitch clip and posts the clip link.

</command>

</commands>

<related>

- [Command errors](../errors/commands.md).
- [Getting started](../setup/getting-started.md).
- [Commands setup](../setup/commands.md).
- [Connection errors](../errors/connection.md).
- [Human guide](https://itsbagelbot.com/guides/commands/).

</related>

</page>
