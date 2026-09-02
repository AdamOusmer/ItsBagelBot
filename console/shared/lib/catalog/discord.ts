// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const DISCORD_MODULE: ModuleDef = {
  id: 'discord',
  // Beta: premium-only until the Discord gateway (welcomes, auto-voice, slash
  // commands) ships. Mirrors .Beta() on the sesame module
  // (app/sesame/modules/discord.go); both flip together.
  beta: true,
  label: 'Discord',
  tagline: 'One bot on Twitch and Discord. Go-live, clips, and a community server.',
  description:
    'Connect an existing Discord server or create one from Bagel’s template. Go-live and clips post from outgress directly — they never wait on sesame. Raids, gift bombs, and milestone subs copy into #announcements when you turn those on. Welcome, auto-voice, and slash commands land with the Discord gateway.',
  icon: 'server',
  category: 'Gear',
  defaultEnabled: false,
  href: '/discord',
  replies: []
};
