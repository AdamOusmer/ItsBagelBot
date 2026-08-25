// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { ModuleDef } from './module-def';

export const QUEUE_MODULE: ModuleDef = 
{
  id: 'queue',
  label: 'Play Queue',
  tagline: 'Let viewers line up to play with you, first come first served.',
  description:
    'Viewers type !join to get in line and !list to see who is next (the first 10). You (and your mods) run the line from chat: !queue open and !queue close accept or stop new joins, !queue next pulls up the next player, !queue remove <user> takes someone out, and !queue clear empties it. Viewers can step out any time with !leave. Turn the module on to enable the commands; the line survives closing so you can play through everyone already waiting.',
  icon: 'users',
  category: 'Play',
  defaultEnabled: false,
  // The conversational replies are customizable per broadcaster; the roster
  // (!list), the status readout and the system/error lines stay fixed (see
  // app/sesame/modules/queue.go). The command list below is read-only. Each
  // reply rehearses as its command (a viewer types the trigger, the bot
  // answers) with this reply's own sample values.
  replies: [
    {
      key: 'join',
      label: 'Join confirmation',
      tagline: 'When a viewer joins the line.',
      event: '!join',
      command: 'join',
      messageKey: 'joinMessage',
      defaultMessage: '@{user} you joined the queue at position #{pos}.',
      tokens: ['user', 'pos'],
      previewSamples: { user: 'sesame_sam', pos: '3' }
    },
    {
      key: 'already',
      label: 'Already in queue',
      tagline: 'When a viewer who is already in line types !join again.',
      event: '!join',
      command: 'join',
      messageKey: 'alreadyMessage',
      defaultMessage: '@{user} you are already in the queue at position #{pos}.',
      tokens: ['user', 'pos'],
      previewSamples: { user: 'sesame_sam', pos: '2' }
    },
    {
      key: 'leave',
      label: 'Leave confirmation',
      tagline: 'When a viewer steps out of the line.',
      event: '!leave',
      command: 'leave',
      messageKey: 'leaveMessage',
      defaultMessage: '@{user} you left the queue.',
      tokens: ['user'],
      previewSamples: { user: 'sesame_sam' }
    },
    {
      key: 'next',
      label: 'Next player up',
      tagline: 'When you pull up the next player.',
      event: '!queue next',
      command: 'queue next',
      messageKey: 'nextMessage',
      defaultMessage: '@{target} you are up next! ({count} still waiting)',
      tokens: ['target', 'count'],
      previewSamples: { target: 'ferret_king', count: '2' }
    },
    {
      key: 'opened',
      label: 'Queue opened',
      tagline: 'Announced when you open the queue.',
      event: '!queue open',
      command: 'queue open',
      messageKey: 'openedMessage',
      defaultMessage: 'The queue is now open! Type !join to get in line.',
      previewSamples: {}
    },
    {
      key: 'closed',
      label: 'Queue closed',
      tagline: 'Announced when you close the queue.',
      event: '!queue close',
      command: 'queue close',
      messageKey: 'closedMessage',
      defaultMessage: 'The queue is now closed to new joins.',
      previewSamples: {}
    }
  ],
  commands: [
    { trigger: '!join', summary: 'Get in line to play (also !queue join).' },
    { trigger: '!leave', summary: 'Step out of the line (also !queue leave).' },
    { trigger: '!list', summary: 'Show the next 10 players waiting (also !queue list).' },
    { trigger: '!queue', summary: 'Show whether the queue is open and how many are waiting.' },
    { trigger: '!queue open', summary: 'Start accepting joins.', perm: 'mod' },
    { trigger: '!queue close', summary: 'Stop accepting joins; the line is kept.', perm: 'mod' },
    { trigger: '!queue next', summary: 'Pull up the next player and announce them.', perm: 'mod' },
    { trigger: '!queue remove <user>', summary: 'Take someone out of the line.', perm: 'mod' },
    { trigger: '!queue clear', summary: 'Empty the line.', perm: 'mod' }
  ]
};
