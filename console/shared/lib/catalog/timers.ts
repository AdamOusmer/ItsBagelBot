// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const TIMERS_MODULE: ModuleDef = 
{
  id: 'timers',
  label: 'Timers',
  tagline: 'Post repeating chat messages on a schedule while you are live.',
  description:
    'Set messages the bot repeats on a schedule while you are live: announcements, socials, reminders. Each timer keeps its own interval and only fires during the stream. Add, edit and arm them on this page.',
  icon: 'clock',
  category: 'Chat',
  defaultEnabled: false,
  href: '/timers',
  // Timers has no standalone grant: the 'commands' grant has always covered
  // it too (see the settings page SECTIONS comment). 'timers' keeps legacy
  // grants minted with a bare timers section working.
  delegateSections: ['modules', 'commands', 'timers'],
  replies: []
};
