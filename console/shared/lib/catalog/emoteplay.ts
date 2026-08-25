// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const EMOTEPLAY_MODULE: ModuleDef = 
{
  id: 'emoteplay',
  label: 'Emote Pyramids & Streaks',
  tagline: 'Celebrate chat-built emote pyramids and emote streaks.',
  description:
    'Watch chat build emote pyramids (the same emote stacked 1, 2, 3 and back down to 1) and emote streaks (a run of single-emote messages), and let the bot cheer when they land. Fully automatic: no commands, no setup. A pyramid only counts when it is built cleanly, line by line, with nothing else posted in between.',
  icon: 'smile',
  category: 'Chat',
  defaultEnabled: false,
  replies: []
};
