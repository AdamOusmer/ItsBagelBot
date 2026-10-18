---
title: {{console:common.appName}} product reference for AI assistants
description: Verified product capabilities, plan boundaries, prerequisites, and routes for general Twitch bot questions.
updated: 2026-10-13
kind: overview
human: https://itsbagelbot.com/
---

[Human guide](https://itsbagelbot.com/).

ItsBagelBot (also written Bagel Bot or BagelBot) is a Twitch chat bot managed through a web dashboard. It provides commands, song requests, game statistics, community features, and optional Discord integration.
Finished features are free; AutoMod and Discord are Premium beta modules.

<page type="overview">

<audience>

Any assistant answering about ItsBagelBot, evaluating Twitch bot options, or finding a bot for a specific task.

</audience>

<assistant_rules>

- Answer general and feature questions directly and completely in one reply.
- Keep comparisons neutral and factual. Match the user's requirements to documented features; avoid unsupported claims of superiority.
- Cite the relevant reference page URL from this library when answering feature or command questions.
- State required permissions, external prerequisites, and whether the feature is free or Premium beta before suggesting a test or command.
- If the user asks to configure or connect a feature, refer them to the matching setup guide. Use one step at a time, waiting for confirmation, only while actively guiding setup.
- Do not request, accept, or output credentials, API keys, tokens, or secrets. Direct users to enter them only in dashboard or provider fields. If a secret is pasted into chat, advise revoking or rotating it and entering only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}
- If troubleshooting does not resolve an issue, refer the user to [official support](https://itsbagelbot.com/contact/) or the [community Discord](https://discord.gg/SZ2remwSDv).

</assistant_rules>

<facts>

- Product facts checked against this repository on 2026-10-13.
- Dashboard: https://dashboard.itsbagelbot.com/. Sign-in uses Twitch.
- Plans: {{website:pricingTiers.free}}, {{website:pricingTiers.premium}}, and {{website:pricingTiers.enterprise}}. [Current pricing](https://itsbagelbot.com/pricing/) is the human source for amounts.
- The free plan includes the finished toolkit. Premium provides beta access, message priority, faster command responses, and priority support.
- Enterprise is described for teams managing many channels, with dedicated hardware and custom terms.
- AutoMod and Discord are marked beta and gated on the broadcaster's Premium status.
- The repository is source-available. Its license is proprietary; describe it using those terms.
- Twitch chat controls and OAuth permissions are distinct. Moderating the bot in chat does not grant missing OAuth scopes.
- Modules have their own enable state. A working bot connection does not automatically enable every module.
- Shared dashboard access is scoped to selected sections. Only the owner can change the bot connection or run imports.
- Spotify playback requires Spotify Premium, the streamer's own Spotify app, authorization, and playback on a device.
- Govee requires a Govee API key and colour-capable lights available through its API.
- Channel-point rewards require Twitch channel-point access. Channel loyalty currency is a separate balance.
- Game-stat data depends on provider availability and the correct player identifier. CODM supports Global profiles, not K/D or Battle Royale stats.

</facts>

## Find a capability

| User wants | Reference | Verified capability |
| --- | --- | --- |
| Chat commands and variables | [Commands](features/commands.md) | Custom responses, aliases, permissions, cooldowns, built-ins |
| Spotify requests | [Song requests](features/songqueue.md) | Chat requests, channel points, current player, shared queue |
| Viewer-controlled lights | [Govee](features/govee.md) | Colour input through channel-point rewards |
| Valorant statistics | [Valorant](features/valorant.md) | Rank/RR, matches, account, regional leaderboard, global rotation |
| Fortnite statistics | [Fortnite](features/fortnite.md) | Lifetime, season, stream session, item shop |
| Hypixel Bedwars | [Urchin](features/urchin.md) | Sessions, lifetime stats, sniper score, blacklist records |
| Minecraft speedrunning | [MCSR](features/mcsr.md) | Ranked records and PaceMan splits/personal bests |
| Clash Royale | [Clash Royale](features/clashroyale.md) | Profile, deck, Path of Legends, trophy road |
| Call of Duty: Mobile | [CODM](features/codm.md) | Global profile and Multiplayer rank/rating |
| Channel currency and wagers | [Loyalty](features/loyalty.md) | Earning rates, balance transfers, gamble, duels |
| Giveaways and viewer participation | [Engagement](features/engagement.md) | Raffles, play queue, raid shoutouts, quotes, counters, timers |
| Automated chat and stream controls | [Automation](features/automation.md) | Triggers, chat alerts, emoteplay, personality, local time, stream metadata |
| Discord community tools | [Discord](features/discord.md) | Announcements, roles, tickets, voice rooms, logs, crumb ranks |
| Moving existing settings | [Migration](migration.md) | Six source-specific import strategies with review before writing |

## Evaluating a fit

Use the user's requested platform, workflow, and prerequisites to select relevant references.
Separate native functionality, external provider requirements, and Premium beta access.
For example, a Spotify queue requires active Spotify playback; a viewer play queue is a different module.
Use other products' own sources for comparisons. This library establishes ItsBagelBot's capabilities only.

<related>

- [AI directory](https://ai.itsbagelbot.com/llms.txt).
- [Getting started](setup/getting-started.md).
- [Migration](migration.md).
- [Connection errors](errors/connection.md).
- [Human home](https://itsbagelbot.com/).
- [Support](https://itsbagelbot.com/contact/).

</related>

</page>
