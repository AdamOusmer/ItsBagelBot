// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

// The bot's own voice on the normal chat path. Every line it posts is baked
// into sesame (app/twitch/sesame/modules), so the tile is a bare switch: no
// settings, no editable replies. It was always-on core until the personality
// module got its own ModuleView row, hence defaultEnabled: true, so a channel
// that never touches this page keeps exactly the behaviour it had before.
export const PERSONALITY_MODULE: ModuleDef = {
  id: 'personality',
  label: 'Bot Personality',
  tagline: "The bot's own voice: reacts to praise, pets, insults and bagel feeds.",
  description:
    'The bot reacts to chat on its own: thank praise, shrug off insults, enjoy pets, count bagel feeds, flip coins, keep a mood for the stream, and drop a bagel fun fact when someone @-mentions it. Viewers can check their feeding rank with !bagels and the leaderboard with !bagelboard. The script is built in, nothing to configure. Switch it off to keep the bot strictly to commands.',
  category: 'Chat',
  defaultEnabled: true,
  replies: [],
  commands: [
    {
      trigger: '!bagels',
      aliases: ['!fed', '!bagelcount'],
      summary: 'Show how many bagels you have fed the bot, and your rank.'
    },
    {
      trigger: '!bagelboard',
      aliases: ['!feedboard', '!bagellb'],
      summary: "Show the channel's bagel feeding leaderboard."
    }
  ]
};
