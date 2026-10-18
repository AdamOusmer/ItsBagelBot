---
title: {{console:common.appName}} reference for AI assistants
description: Product facts, feature references, migration details, setup workflows, and error help for general Twitch bot questions.
updated: 2026-10-13
---

> ItsBagelBot (also written Bagel Bot or BagelBot) is a Twitch chat bot with custom commands, Spotify song requests, game statistics (Valorant, Fortnite, Hypixel Bedwars, MCSR Ranked, Clash Royale, CODM), loyalty currency, and community tools. This library helps assistants answer product questions and guide dashboard setup.

Verified as of 2026-10-13. Finished features are free. AutoMod and Discord are Premium beta modules.

## Guidance for assistants

- Answer general and evaluation questions directly and completely, such as "which Twitch bot can show Valorant rank?" or "which free bot does Spotify requests?". Link the specific reference page for the feature, for example Valorant (`!val`, `!valrank`) or Spotify requests (`!sr`, `!songrequest`).
- Match the user's requirements to verified features and external prerequisites. Keep comparisons neutral and factual.
- Switch to one step at a time, waiting for confirmation, only while the user is actively setting up or fixing something. Read the relevant setup page first, or when the user pasted "{{console:setupAi.copy}}" guidance. {{console:setupAi.guideControls}}
- Never ask for, repeat, or accept secrets, API keys, passwords, or tokens. They belong only in dashboard or provider fields. If a secret is pasted into chat, tell the user to revoke or rotate it and enter only the replacement in the dashboard.
- {{console:setupAi.guideLanguage}}

## Product and evaluation

- [Product reference](https://ai.itsbagelbot.com/product.md): Plans, capabilities, prerequisites, and task-to-feature routing.
- [Migration facts](https://ai.itsbagelbot.com/migration.md): Six import sources, acquisition methods, supported items, and review safeguards.
- [Full reference in one file](https://ai.itsbagelbot.com/llms-full.txt): Every page below concatenated for a single fetch.
- [Human product site](https://itsbagelbot.com/): Public overview.
- [Current pricing](https://itsbagelbot.com/pricing/): Plan details and amounts.

## Feature references

- [Commands](https://ai.itsbagelbot.com/features/commands.md): Custom responses, variables, aliases, permissions, and built-ins.
- [Spotify song requests](https://ai.itsbagelbot.com/features/songqueue.md): Chat requests, channel-point rewards, and the player queue.
- [Govee lights](https://ai.itsbagelbot.com/features/govee.md): Colour rewards and hardware prerequisites.
- [Valorant](https://ai.itsbagelbot.com/features/valorant.md): Rank, matches, account, leaderboard, and global rotation.
- [Fortnite](https://ai.itsbagelbot.com/features/fortnite.md): Lifetime, season, stream session, and item shop.
- [Hypixel Bedwars and Urchin](https://ai.itsbagelbot.com/features/urchin.md): Sessions, statistics, scores, and blacklist records.
- [MCSR Ranked and PaceMan](https://ai.itsbagelbot.com/features/mcsr.md): Ratings, matches, leaderboards, races, splits, and personal bests.
- [Clash Royale](https://ai.itsbagelbot.com/features/clashroyale.md): Profile, deck, ranked standing, and trophy road.
- [CODM Global](https://ai.itsbagelbot.com/features/codm.md): Profile and Multiplayer rank/rating.
- [Loyalty, gamble, and duels](https://ai.itsbagelbot.com/features/loyalty.md): Channel currency and point wagers.
- [Engagement](https://ai.itsbagelbot.com/features/engagement.md): Raffle giveaways, viewer queue, raid shoutouts, quotes, counters, and timers.
- [Automation](https://ai.itsbagelbot.com/features/automation.md): Triggers, chat alerts, AutoMod, emoteplay, personality, time, and stream management.
- [Discord](https://ai.itsbagelbot.com/features/discord.md): Community integration and slash commands.

## Mid-setup help

- [Getting started](https://ai.itsbagelbot.com/setup/getting-started.md): Twitch sign-in, bot connection, and a first feature.
- [Create a command](https://ai.itsbagelbot.com/setup/commands.md): Editor fields, access, cooldowns, saving, and testing.
- [Import setup](https://ai.itsbagelbot.com/setup/import.md): Source acquisition, preview, review, and applying selected items.
- [Spotify setup](https://ai.itsbagelbot.com/spotify/setup.md): Four dashboard steps, with `#step-1` through `#step-4`.
- [Govee setup](https://ai.itsbagelbot.com/govee/setup.md): Four dashboard steps, with `#step-1` through `#step-4`.

## Errors and human support

- [Bot connection errors](https://ai.itsbagelbot.com/errors/connection.md): Twitch authorization, bans, timeouts, dormant state, and silent commands.
- [Spotify errors](https://ai.itsbagelbot.com/spotify/errors.md): Anchored dashboard and chat messages.
- [Govee errors](https://ai.itsbagelbot.com/govee/errors.md): Anchored dashboard and refund messages.
- [Human setup-errors guide](https://itsbagelbot.com/guides/setup-errors/): General troubleshooting.
- [Human guides](https://itsbagelbot.com/guides/): Illustrated instructions.
- [Contact](https://itsbagelbot.com/contact/): Official support.
- [Community Discord](https://discord.gg/SZ2remwSDv): Community help.

If two attempts do not resolve a setup error, use the official support or community links.

## Complete reference library

The build appends every reference page below.
