// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FieldSpec, NodeSpec } from './model';

const target: FieldSpec = { key: 'target', type: 'template', singleLine: true };
const title: FieldSpec = { key: 'title', type: 'template', singleLine: true };
const toggle: FieldSpec = { key: 'on', type: 'toggle' };

export const STREAM_STEPS: readonly NodeSpec[] = [
  {
    type: 'twitch.unban_request', slug: 'unbanRequest', kind: 'trigger', group: 'twitch', icon: 'moderation', fields: []
  },
  {
    type: 'reply.poll', slug: 'poll', kind: 'reply', group: 'twitch', icon: 'messageSquare',
    fields: [title, { key: 'choices', type: 'template' }, { key: 'seconds', type: 'number', min: 15, max: 1800 }]
  },
  {
    type: 'reply.poll_end', slug: 'pollEnd', kind: 'reply', group: 'twitch', icon: 'messageSquare',
    fields: [{ key: 'status', type: 'select', options: ['terminate', 'archive'] }]
  },
  {
    type: 'reply.prediction', slug: 'prediction', kind: 'reply', group: 'twitch', icon: 'gem',
    fields: [title, { key: 'outcomes', type: 'template' }, { key: 'seconds', type: 'number', min: 30, max: 1800 }]
  },
  {
    type: 'reply.prediction_end', slug: 'predictionEnd', kind: 'reply', group: 'twitch', icon: 'gem',
    fields: [
      { key: 'status', type: 'select', options: ['lock', 'cancel', 'resolve'] },
      { key: 'winner', type: 'number', min: 1, max: 10, when: { key: 'status', value: 'resolve' } }
    ]
  },
  {
    type: 'reply.raid', slug: 'raidStart', kind: 'reply', group: 'twitch', icon: 'swords', fields: [target]
  },
  {
    type: 'reply.raid_cancel', slug: 'raidCancel', kind: 'reply', group: 'twitch', icon: 'swords', fields: []
  },
  {
    type: 'reply.mod', slug: 'mod', kind: 'reply', group: 'twitch', icon: 'moderation', fields: [target, toggle]
  },
  {
    type: 'reply.vip', slug: 'vip', kind: 'reply', group: 'twitch', icon: 'star', fields: [target, toggle]
  },
  {
    type: 'reply.chat_mode', slug: 'chatMode', kind: 'reply', group: 'twitch', icon: 'messageSquare',
    fields: [
      { key: 'mode', type: 'select', options: ['emote_only', 'followers', 'slow', 'subscribers', 'unique'] },
      toggle,
      { key: 'value', type: 'number', min: 0, max: 86400, when: { key: 'mode', value: ['followers', 'slow'] } }
    ]
  },
  {
    type: 'reply.clear_chat', slug: 'clearChat', kind: 'reply', group: 'twitch', icon: 'messageSquare', fields: []
  },
  {
    type: 'reply.ad_snooze', slug: 'adSnooze', kind: 'reply', group: 'twitch', icon: 'timer', fields: []
  },
  {
    type: 'reply.whisper', slug: 'whisper', kind: 'reply', group: 'twitch', icon: 'send',
    fields: [target, { key: 'text', type: 'template' }]
  },
  {
    type: 'reply.unban_resolve', slug: 'unbanResolve', kind: 'reply', group: 'twitch', icon: 'moderation',
    fields: [{ key: 'status', type: 'select', options: ['approve', 'deny'] }, { key: 'resolution', type: 'template' }]
  },
  {
    type: 'reply.branded_content', slug: 'brandedContent', kind: 'reply', group: 'twitch', icon: 'radio', fields: [toggle]
  }
];
