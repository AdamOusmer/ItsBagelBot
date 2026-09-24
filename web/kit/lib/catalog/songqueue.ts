// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { replyTokens, type ModuleDef } from './module-def';

export const SONGQUEUE_MODULE: ModuleDef = {
  id: 'songqueue',
  label: 'Song Requests',
  tagline: '!sr and a channel-points reward that queue songs from Spotify.',
  description:
    'Register your own Spotify app and connect your account to it once, then let viewers queue music two ways: the !sr chat command (with a permission tier you pick, from everyone down to just you) and a channel-points reward whose typed input is the song query: a name, "artist - song", or a pasted Spotify link. The bot announces what is playing, resolves links and search names against Spotify, and keeps one song per viewer in the up-next list. !current reads your Spotify player directly, so it answers even for tracks nobody requested. Moderators manage the queue with !sr next, remove and clear.',
  category: 'Gear',
  defaultEnabled: false,
  href: '/songqueue',
  delegateSections: ['modules', 'channelpoints'],
  replies: [
    {
      key: 'redeem',
      label: 'Redeem reply',
      tagline: 'What the bot posts when the queue reward is redeemed.',
      event: 'on redemption',
      messageKey: 'replyMessage',
      defaultMessage: '@{user} queued {track}, position #{pos}.',
      tokens: replyTokens(
        ['user', 'track', 'input', 'pos'],
        { user: 'sesame_sam', track: 'Song Title', input: 'song title', pos: '3' },
        'songqueue.redeem'
      )
    }
  ],
  commands: [
    {
      trigger: '!sr',
      summary: 'Show now playing, or queue a track by name, "artist - song", or a Spotify link.'
    },
    { trigger: '!remove', summary: 'Take back your queued request, or drop a position as a mod.' },
    {
      trigger: '!skip',
      summary: 'Mark the current track played and promote the next (also !next).',
      perm: 'mod'
    },
    { trigger: '!clear', summary: 'Empty the queue.', perm: 'mod' },
    { trigger: '!srlist', summary: 'Show the next five songs in the queue (also !songlist).' },
    {
      trigger: '!current',
      summary: 'Say what your Spotify is playing right now (also !song, !np).'
    }
  ]
};
