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
  href: '/discord',
  replies: []
};
