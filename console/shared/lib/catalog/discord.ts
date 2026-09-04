// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const DISCORD_MODULE: ModuleDef = {
  id: 'discord',
  label: 'Discord',
  tagline: 'One bot on Twitch and Discord. Go-live, clips, welcomes, tickets, and voice.',
  description:
    'Connect an existing Discord server or create one from Bagel’s template. Go-live and clips post from outgress directly — they never wait on sesame. Raids, gift bombs, and milestone subs copy into #announcements when you turn those on. The Discord gateway (dingress) handles welcomes, join-to-create voice, support tickets, staff logs, slash moderation, and crumb ranks.',
  icon: 'server',
  category: 'Gear',
  defaultEnabled: false,
  // Premium-only while Discord is in beta. This flag drives three things
  // through machinery that already exists: the Beta chip, the locked tile,
  // and -- because href is '/discord' -- betaRouteDef closing the whole
  // section and its form actions in the route guard.
  //
  // It is only half the gate. The other half is BetaPremiumOnly in
  // internal/domain/discord/beta.go, which stops the bot serving a guild
  // whose row was enabled before the gate existed; a locked page does not
  // unenrol anyone. discord-beta-gate.test.ts fails if the two disagree.
  beta: true,
  href: '/discord',
  replies: [],
  // Discord is its own sidebar section now (see DASHBOARD_SECTIONS in nav.ts),
  // so its bespoke page rides its own grant instead of the default 'modules'
  // fallback. A pre-existing 'modules' delegation no longer opens /discord —
  // intended, but a real behavior change; see the GRANTABLE_SECTIONS comment.
  delegateSections: ['discord']
};
