// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const SONGQUEUE_MODULE: ModuleDef = {
  id: 'songqueue',
  label: 'Song Requests',
  tagline: '!sr and a channel-points reward that queue songs from Spotify.',
  description:
    'Connect your Spotify account once, then let viewers queue music two ways: the !sr chat command (with a permission tier you pick, from everyone down to just you) and a channel-points reward whose typed input is the song query — a name, "artist - song", or a pasted Spotify link. The bot announces what is playing, resolves links and search names against Spotify, and keeps one song per viewer in the up-next list. Moderators manage the queue with !sr next, remove and clear.',
  icon: 'music',
  category: 'Gear',
  defaultEnabled: false,
  // The generic reply page cannot express OAuth custody + the reward editor,
  // so the tile opens the bespoke songqueue page instead.
  href: '/songqueue',
  replies: [],
  // The engine registers one command (`sr`, aliases songrequest/songreq) and
  // routes leading verbs. The ledger shows those verbs the way chat types
  // them — !sr, !remove, !next, !clear — not the long spellings.
  commands: [
    {
      trigger: '!sr',
      summary: 'Show now playing, or queue a track by name, artist – song, or Spotify link.'
    },
    { trigger: '!remove', summary: 'Take back your queued request, or drop a position as a mod.' },
    {
      trigger: '!next',
      summary: 'Mark the current track played and promote the next.',
      perm: 'mod'
    },
    { trigger: '!clear', summary: 'Empty the queue.', perm: 'mod' }
  ]
};
