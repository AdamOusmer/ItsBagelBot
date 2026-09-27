---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Module variables in custom commands
description: Read public module values from custom command responses with module:variable syntax and explicit module enablement requirements.
---

Custom commands can read public module values using `{module:variable}`. The
separator is a colon. These values come from the same public fields used in a
module's replies; they do not expose private configuration or credentials.

## Make a Valorant rank command

1. Open **Modules → Valorant Stats**, enable the module and configure the Riot
   account and region it requires.
2. Create a custom command named `rank` in **Commands**.
3. Set its response to `My Valorant rank is {valorant:tier} ({valorant:rr} RR).`
4. Save the command. Viewers can now type `!rank`.

The Valorant module must be enabled. Creating a command or inserting its variables
does not enable the module. This requirement applies to every module namespace.
Modules enabled by default keep their normal default; opt-in modules must be
turned on. Premium or beta restrictions still apply.
Gamble and Duels also require their parent **Loyalty Points** module to be enabled.

If the module is disabled or unavailable, the variable stays visible in the
response, and the bot does not fetch that module's data. A fallback does not bypass
this gate. For an enabled module, a fallback such as
`{valorant:tier|rank unavailable}` supplies text when the requested value is empty.

The editor's **Module variables** picker lists the available spellings and tells
the author which module needs to be enabled. The rehearsal uses example values;
it does not check the channel's module state or connected account.

Timers can also read module variables. They have no triggering viewer, so
viewer-specific fields are empty. Configure an account explicitly for providers
that normally infer it from the broadcaster's login; timers do not carry that
login. Existing premium and module requirements still apply.

## Other module namespaces

| Namespace | Example | Module |
| --- | --- | --- |
| `accountage` | `{accountage:accountage}` | Account age |
| `alerts` | `{alerts:user}` | Chat Alerts |
| `channelpoints` | `{channelpoints:reward}` | Channel Points |
| `clashroyale` | `{clashroyale:trophies}` | Clash Royale Stats |
| `clip` | `{clip:clip}` | Clip |
| `codm` | `{codm:rank}` | CODM Profile |
| `commercial` | `{commercial:length}` | Commercial |
| `duel` | `{duel:winner}` | Duels |
| `followage` | `{followage:followage}` | Followage |
| `fortnite` | `{fortnite:wins}` | Fortnite Stats |
| `gamble` | `{gamble:points}` | Gamble |
| `game` | `{game:game}` | Game |
| `govee` | `{govee:color}` | Govee Lights |
| `loyalty` | `{loyalty:points}` | Loyalty Points |
| `marker` | `{marker:user}` | Marker |
| `mcsr` | `{mcsr:elo}` | MCSR Ranked |
| `personality` | `{personality:user}` | Chat personality |
| `queue` | `{queue:size}` | Play Queue |
| `quotes` | `{quotes:text}` | Quotes |
| `raffle` | `{raffle:entrants}` | Raffle |
| `shoutout` | `{shoutout:raider}` | Auto Shoutout |
| `songqueue` | `{songqueue:title}` | Song Requests |
| `stream` | `{stream:uptime}` | Stream Management |
| `tags` | `{tags:tags}` | Tags |
| `time` | `{time:time}` | Local Time |
| `title` | `{title:title}` | Title |
| `triggers` | `{triggers:user}` | Trigger Words |
| `uptime` | `{uptime:uptime}` | Uptime |
| `urchin` | `{urchin:wins}` | Bedwars Stats |
| `valorant` | `{valorant:tier}` | Valorant Stats |

Only published variables are available. A module with no public reply fields or
readable command facts has no variables to insert.

When a field exists in multiple reply views, the short spelling reads the first
published view. Use `{module:view:variable}` to select a particular view, for
example `{valorant:rank:tier}` for the rank view. The picker includes both forms.
Existing bare command variables keep their original syntax. Module reply
editors show namespaced fields and convert recognized legacy fields on save.

Some fields describe an event, such as a raid's raider or a reward redemption's
input. A normal custom command has no matching event, so those fields are empty;
it does not recover a previous event or trigger a redemption. Reading game,
queue, raffle or loyalty facts does not execute the module's command or its
actions. Session lookups retain the provider's usual baseline behavior. Use the
variable reference and picker to see every available field.

## Saved module templates

The module editors display legacy saved fields in the new namespaced syntax.
For example, a saved Valorant reply containing `{tier|unranked}` opens as
`{valorant:tier|unranked}`. The change is saved with the edited reply; simply
opening an editor does not write the stored configuration. Unknown tokens,
dynamic variables, fallback text and conditional branch text are preserved.
Already namespaced templates remain unchanged. The runtime accepts legacy
module fields during the transition.
