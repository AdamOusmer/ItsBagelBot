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

export const STREAM_STEP_DEFINITIONS: readonly StepDefinition[] = [
  {
    type: 'twitch.stream.offline', slug: 'streamOffline', icon: 'radio', fields: []
  },
  {
    type: 'reply.shoutout', slug: 'shoutout', icon: 'send', fields: [target]
  },
  {
    type: 'reply.announce', slug: 'announce', icon: 'messageSquare',
    fields: [{ key: 'text', type: 'template' }, { key: 'color', type: 'select', options: ['primary', 'blue', 'green', 'orange', 'purple'] }]
  },
  {
    type: 'reply.reply', slug: 'reply', icon: 'messageSquare', hidden: true,
    fields: [{ key: 'text', type: 'template' }]
  },
  {
    type: 'reply.timeout', slug: 'timeout', icon: 'moderation',
    fields: [...TARGET_FIELDS, { key: 'seconds', type: 'duration', min: 1, max: 1209600, initial: 600 }, { key: 'reason', type: 'template' }]
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
    fields: [
      title, { key: 'choices', type: 'template' }, { key: 'seconds', type: 'number', min: 15, max: 1800, initial: 60 },
      { key: 'extraVotes', type: 'toggle', initial: false },
      { key: 'pointsPerVote', type: 'number', min: 1, max: 1000000, initial: 100, when: { key: 'extraVotes', value: true } }
    ]
  },
  {
    type: 'reply.poll_end', slug: 'pollEnd', icon: 'messageSquare', lineBy: 'status',
    fields: [{ key: 'status', type: 'select', options: ['terminate', 'archive'] }]
  },
  {
    type: 'reply.prediction', slug: 'prediction', icon: 'gem',
    fields: [title, { key: 'outcomes', type: 'template' }, { key: 'seconds', type: 'number', min: 1, max: 1800, initial: 120 }]
  },
  {
    type: 'reply.prediction_end', slug: 'predictionEnd', icon: 'gem', lineBy: 'status',
    fields: [
      { key: 'status', type: 'select', options: ['lock', 'cancel', 'resolve'] },
      { key: 'outcome', type: 'template', singleLine: true, when: { key: 'status', value: 'resolve' } }
    ]
  },
  {
    type: 'reply.raid', slug: 'raidStart', icon: 'swords', fields: [target]
  },
  {
    type: 'reply.raid_cancel', slug: 'raidCancel', icon: 'swords', fields: []
  },
  {
    type: 'reply.mod', slug: 'mod', icon: 'moderation', fields: [...TARGET_FIELDS, on], lineBy: 'on'
  },
  {
    type: 'reply.vip', slug: 'vip', icon: 'star', fields: [...TARGET_FIELDS, on], lineBy: 'on'
  },
  {
    type: 'reply.chat_mode', slug: 'chatMode', icon: 'messageSquare', lineBy: 'on',
    fields: [
      { key: 'mode', type: 'select', options: ['slow', 'followers', 'subscribers', 'emote_only', 'unique', 'chat_delay'], initial: 'slow' },
      on,
      { key: 'wait', type: 'duration', min: 3, max: 120, initial: 30, when: [{ key: 'mode', value: 'slow' }, { key: 'on', value: true }] },
      { key: 'followAge', type: 'number', min: 0, max: 129600, unit: true, initial: 10, when: [{ key: 'mode', value: 'followers' }, { key: 'on', value: true }] },
      { key: 'delay', type: 'select', options: ['2', '4', '6'], initial: '2', when: [{ key: 'mode', value: 'chat_delay' }, { key: 'on', value: true }] }
    ]
  },
  {
    type: 'reply.clear_chat', slug: 'clearChat', icon: 'messageSquare', fields: []
  },
  {
    type: 'reply.delete_message', slug: 'deleteMessage', icon: 'messageSquare', fields: []
  },
  {
    type: 'reply.pin', slug: 'pin', icon: 'messageSquare', fields: [{ key: 'text', type: 'template', singleLine: true, max: 500 }]
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
    fields: [{ key: 'status', type: 'select', options: ['approve', 'deny'], initial: 'deny' }, { key: 'resolution', type: 'template' }]
  },
  {
    type: 'reply.branded_content', slug: 'brandedContent', icon: 'radio', fields: [{ key: 'on', type: 'toggle' }]
  }
];
