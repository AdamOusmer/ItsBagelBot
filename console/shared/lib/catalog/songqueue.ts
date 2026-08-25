// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const SONGQUEUE_MODULE: ModuleDef = {
  id: 'songqueue',
  label: 'Song Requests',
  tagline: '!sr and a channel-points reward that queue songs from Spotify.',
  description:
    'Register your own Spotify app and connect your account to it once, then let viewers queue music two ways: the !sr chat command (with a permission tier you pick, from everyone down to just you) and a channel-points reward whose typed input is the song query — a name, "artist - song", or a pasted Spotify link. The bot announces what is playing, resolves links and search names against Spotify, and keeps one song per viewer in the up-next list. !current reads your Spotify player directly, so it answers even for tracks nobody requested. Moderators manage the queue with !sr next, remove and clear.',
  icon: 'music',
  category: 'Gear',
  defaultEnabled: false,
  // The generic reply page cannot express OAuth custody + the reward editor,
  // so the tile opens the bespoke songqueue page instead.
  href: '/songqueue',
  // A channel-points delegate opens this page too: the reward that queues a
  // song is created and edited here, not on the Channel Points tab, so
  // scoping it to 'modules' alone would lock out the person who manages the
  // rewards. (govee carries the same pair for the same reason.)
  delegateSections: ['modules', 'channelpoints'],
  replies: [],
  // The engine registers `sr` (aliases songrequest/songreq), which routes
  // leading verbs, plus the standalone `song` (aliases current/nowplaying/np),
  // `skip` (alias next), `clear` and `remove`. The standalones matter: without
  // them !skip and !clear fall through to a custom command that replies
  // without ever touching the queue. The ledger shows the spellings chat
  // types — !sr, !remove, !skip, !clear — not the long forms.
  commands: [
    {
      trigger: '!sr',
      summary: 'Show now playing, or queue a track by name, artist – song, or Spotify link.'
    },
    { trigger: '!remove', summary: 'Take back your queued request, or drop a position as a mod.' },
    {
      trigger: '!skip',
      summary: 'Mark the current track played and promote the next (also !next).',
      perm: 'mod'
    },
    { trigger: '!clear', summary: 'Empty the queue.', perm: 'mod' },
    {
      trigger: '!current',
      summary: 'Say what your Spotify is playing right now (also !song, !np).'
    }
  ]
};
