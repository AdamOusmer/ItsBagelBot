// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FieldSpec, StepDefinition } from './model';

const target: FieldSpec = { key: 'target', type: 'template', singleLine: true };
const title: FieldSpec = { key: 'title', type: 'template', singleLine: true };
const toggle: FieldSpec = { key: 'on', type: 'toggle' };

export const STREAM_STEP_DEFINITIONS: readonly StepDefinition[] = [
  {
    type: 'twitch.stream.offline', slug: 'streamOffline', icon: 'radio', fields: []
  },
  {
    type: 'reply.shoutout', slug: 'shoutout', icon: 'send', fields: [target]
  },
  {
    type: 'reply.announce', slug: 'announce', icon: 'messageSquare',
    fields: [{ key: 'text', type: 'template' }, { key: 'color', type: 'select', options: ['primary', 'blue', 'green', 'orange', 'purple', ''] }]
  },
  {
    type: 'reply.reply', slug: 'reply', icon: 'messageSquare',
    fields: [{ key: 'text', type: 'template' }]
  },
  {
    type: 'reply.timeout', slug: 'timeout', icon: 'moderation',
    fields: [target, { key: 'seconds', type: 'duration', min: 1, max: 1209600 }, { key: 'reason', type: 'template' }]
  },
  {
    type: 'twitch.unban_request', slug: 'unbanRequest', icon: 'moderation', fields: []
  },
  {
    type: 'reply.poll', slug: 'poll', icon: 'messageSquare',
    fields: [title, { key: 'choices', type: 'template' }, { key: 'seconds', type: 'number', min: 15, max: 1800 }]
  },
  {
    type: 'reply.poll_end', slug: 'pollEnd', icon: 'messageSquare',
    fields: [{ key: 'status', type: 'select', options: ['terminate', 'archive'] }]
  },
  {
    type: 'reply.prediction', slug: 'prediction', icon: 'gem',
    fields: [title, { key: 'outcomes', type: 'template' }, { key: 'seconds', type: 'number', min: 30, max: 1800 }]
  },
  {
    type: 'reply.prediction_end', slug: 'predictionEnd', icon: 'gem',
    fields: [
      { key: 'status', type: 'select', options: ['lock', 'cancel', 'resolve'] },
      { key: 'winner', type: 'number', min: 1, max: 10, when: { key: 'status', value: 'resolve' } }
    ]
  },
  {
    type: 'reply.raid', slug: 'raidStart', icon: 'swords', fields: [target]
  },
  {
    type: 'reply.raid_cancel', slug: 'raidCancel', icon: 'swords', fields: []
  },
  {
    type: 'reply.mod', slug: 'mod', icon: 'moderation', fields: [target, toggle]
  },
  {
    type: 'reply.vip', slug: 'vip', icon: 'star', fields: [target, toggle]
  },
  {
    type: 'reply.chat_mode', slug: 'chatMode', icon: 'messageSquare',
    fields: [
      { key: 'mode', type: 'select', options: ['emote_only', 'followers', 'slow', 'subscribers', 'unique'] },
      toggle,
      { key: 'value', type: 'number', min: 0, max: 86400, when: { key: 'mode', value: ['followers', 'slow'] } }
    ]
  },
  {
    type: 'reply.clear_chat', slug: 'clearChat', icon: 'messageSquare', fields: []
  },
  {
    type: 'reply.ad_snooze', slug: 'adSnooze', icon: 'timer', fields: []
  },
  {
    type: 'reply.whisper', slug: 'whisper', icon: 'send',
    fields: [target, { key: 'text', type: 'template' }]
  },
  {
    type: 'reply.unban_resolve', slug: 'unbanResolve', icon: 'moderation',
    fields: [{ key: 'status', type: 'select', options: ['approve', 'deny'] }, { key: 'resolution', type: 'template' }]
  },
  {
    type: 'reply.branded_content', slug: 'brandedContent', icon: 'radio', fields: [toggle]
  }
];
