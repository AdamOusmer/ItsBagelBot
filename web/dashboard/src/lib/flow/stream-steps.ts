// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { FieldSpec, StepDefinition } from './model';

const target: FieldSpec = { key: 'target', type: 'template', singleLine: true, required: true };
export const TARGET_FIELDS = [
  { key: 'who', type: 'target' },
  { key: 'login', type: 'login', when: { key: 'who', value: 'user' } },
  { key: 'target', type: 'template', singleLine: true, when: { key: 'who', value: 'login' } }
] as const satisfies readonly FieldSpec[];

const title: FieldSpec = { key: 'title', type: 'template', singleLine: true };
const on: FieldSpec = { key: 'on', type: 'toggle', initial: true };
const change: FieldSpec = { key: 'change', type: 'select', options: ['add', 'remove'], initial: 'add', fallback: 'remove' };

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
    fields: [...TARGET_FIELDS, { key: 'seconds', type: 'duration', min: 1, max: 1209600, initial: 600 }, { key: 'reason', type: 'template' }]
  },
  {
    type: 'reply.unban', slug: 'unban', icon: 'moderation', fields: [...TARGET_FIELDS]
  },
  {
    type: 'reply.ban', slug: 'ban', icon: 'moderation',
    fields: [...TARGET_FIELDS, { key: 'reason', type: 'template' }]
  },
  {
    type: 'twitch.unban_request', slug: 'unbanRequest', icon: 'moderation', fields: []
  },
  {
    type: 'reply.poll', slug: 'poll', icon: 'messageSquare',
    fields: [title, { key: 'choices', type: 'template' }, { key: 'seconds', type: 'number', min: 15, max: 1800, initial: 60 }]
  },
  {
    type: 'reply.poll_end', slug: 'pollEnd', icon: 'messageSquare', lineBy: 'status',
    fields: [{ key: 'status', type: 'select', options: ['terminate', 'archive'] }]
  },
  {
    type: 'reply.prediction', slug: 'prediction', icon: 'gem',
    fields: [title, { key: 'outcomes', type: 'template' }, { key: 'seconds', type: 'number', min: 30, max: 1800, initial: 120 }]
  },
  {
    type: 'reply.prediction_end', slug: 'predictionEnd', icon: 'gem', lineBy: 'status',
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
    type: 'reply.mod', slug: 'mod', icon: 'moderation', fields: [...TARGET_FIELDS, change], lineBy: 'change'
  },
  {
    type: 'reply.vip', slug: 'vip', icon: 'star', fields: [...TARGET_FIELDS, change], lineBy: 'change'
  },
  {
    type: 'reply.chat_mode', slug: 'chatMode', icon: 'messageSquare', lineBy: 'on',
    fields: [
      { key: 'mode', type: 'select', options: ['emote_only', 'followers', 'slow', 'subscribers', 'unique'], initial: 'slow' },
      on,
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
    type: 'reply.unban_resolve', slug: 'unbanResolve', icon: 'moderation', lineBy: 'status',
    fields: [{ key: 'status', type: 'select', options: ['approve', 'deny'], initial: 'deny' }, { key: 'resolution', type: 'template' }]
  },
  {
    type: 'reply.branded_content', slug: 'brandedContent', icon: 'radio', fields: [{ key: 'on', type: 'toggle' }]
  }
];
