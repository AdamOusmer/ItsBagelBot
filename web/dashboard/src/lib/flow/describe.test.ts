// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { describe } from './describe';
import { english } from './english.testing';
import type { FlowNode } from './model';

test.each<{ type: FlowNode['type']; config: FlowNode['config']; line: string; trigger?: Pick<FlowNode, 'type' | 'config'> }>([
  { type: 'countdown.finished', config: { name: 'subathon' }, line: 'When subathon reaches zero' },
  { type: 'reply.poll', config: { title: 'Next game?' }, line: 'Poll: Next game?' },
  { type: 'reply.poll', config: { title: '  ' }, line: 'Let viewers vote on a question' },
  { type: 'reply.prediction', config: { title: 'Will we win?' }, line: 'Prediction: Will we win?' },
  { type: 'reply.poll_end', config: { status: 'terminate' }, line: 'End the poll and show results' },
  { type: 'reply.poll_end', config: { status: 'archive' }, line: 'Archive the poll' },
  { type: 'reply.poll_end', config: { status: '' }, line: 'Stop voting or archive the results' },
  { type: 'reply.prediction_end', config: { status: 'lock' }, line: 'Lock bets' },
  { type: 'reply.prediction_end', config: { status: 'cancel' }, line: 'Cancel and refund points' },
  { type: 'reply.prediction_end', config: { status: 'resolve', winner: 2 }, line: 'Winning outcome: 2' },
  { type: 'reply.prediction_end', config: { status: 'resolve' }, line: 'Lock bets, refund points or pick a winner' },
  { type: 'reply.raid', config: { target: 'lotti' }, line: 'Raid lotti' },
  { type: 'reply.mod', config: { target: 'lotti', on: true }, line: 'Make lotti a mod' },
  { type: 'reply.mod', config: { target: 'lotti', on: false }, line: 'Remove mod from lotti' },
  { type: 'reply.mod', config: { on: true }, line: 'Give or remove moderator access' },
  { type: 'reply.vip', config: { target: 'lotti', on: true }, line: 'Make lotti a VIP' },
  { type: 'reply.vip', config: { target: 'lotti', on: false }, line: 'Remove VIP from lotti' },
  { type: 'reply.chat_mode', config: { mode: 'slow', on: true }, line: 'Slow mode on' },
  { type: 'reply.chat_mode', config: { mode: 'followers', on: false }, line: 'Followers only off' },
  { type: 'reply.chat_mode', config: { mode: '', on: true }, line: 'Change who can chat and how often' },
  { type: 'reply.whisper', config: { target: '{user}', text: 'Hi {target}' }, line: 'Whisper to {user}: Hi {target}' },
  { type: 'reply.whisper', config: { target: 'lotti', text: '' }, line: 'Send a private message to one viewer' },
  { type: 'reply.countdown_start', config: { name: 'subathon', seconds: 3660 }, line: 'Start subathon for 1h 1m' },
  { type: 'reply.countdown_start', config: { name: 'subathon', seconds: '' }, line: 'Start or restart a named countdown' },
  { type: 'reply.countdown_add', config: { name: 'subathon', seconds: 90 }, line: 'Add 1m 30s to subathon' },
  { type: 'reply.cooldown_add', config: { name: 'raffle', seconds: 60 }, line: 'Add 1m to raffle' },
  { type: 'reply.shoutout', config: { target: 'lotti' }, line: 'Shoutout to lotti' },
  { type: 'reply.points', config: { change: 'add', amount: 10 }, line: 'Give 10 points' },
  { type: 'reply.points', config: { change: 'take', amount: 10 }, line: 'Take 10 points' },
  { type: 'reply.points_set', config: { amount: 0 }, line: 'Set points to 0' },
  { type: 'reply.counter', config: { counter: 'wins', change: 'add', amount: 1 }, line: 'Add 1 to wins' },
  { type: 'reply.counter', config: { counter: 'wins', change: 'add', amount: -2 }, line: 'Take 2 from wins' },
  { type: 'reply.counter', config: { counter: 'wins', change: 'set', value: 0 }, line: 'Set wins to 0' },
  { type: 'cond.points', config: { op: 'gte', value: 100 }, line: 'Viewer has at least 100 points' },
  { type: 'cond.counter', config: { counter: 'wins', op: 'gte', value: 1 }, line: 'wins is at least 1' },
  { type: 'reply.redemption', config: { result: 'fulfill' }, line: 'Mark fulfilled' },
  { type: 'reply.redemption', config: { result: 'cancel' }, line: 'Refund the points' },
  { type: 'reply.countdown_start', config: { name: 'subathon', seconds: 3600 }, line: 'Start subathon for 1h' },
  { type: 'reply.countdown_add', config: { name: 'subathon', seconds: 60 }, line: 'Add 1m to subathon' },
  { type: 'reply.countdown_add', config: { name: 'subathon', seconds: 60, per: 100 }, trigger: { type: 'twitch.cheer', config: {} }, line: 'Add 1m to subathon for every 100 bits' },
  { type: 'reply.cooldown_add', config: { name: 'break2', seconds: 60, per: 2 }, trigger: { type: 'twitch.sub', config: { variant: 'gift' } }, line: 'Add 1m to break2 for every 2 gifted subs' },
  { type: 'reply.points_spend', config: { amount: 100 }, line: 'Spend 100 points' },
  { type: 'reply.points_spend', config: { amount: 1 }, line: 'Spend 1 point' },
  { type: 'reply.reward_update', config: { rewardTitle: 'Hydrate', setEnabled: 'off', paused: 'pause', cost: 500, cooldown: 300, reset: true }, line: 'Turn off Hydrate, Pause Hydrate, Set Hydrate to 500 points, Set Hydrate cooldown to 5m until the stream ends' },
  { type: 'reply.reward_update', config: { rewardTitle: 'Hydrate', paused: 'resume' }, line: 'Resume Hydrate' },
  { type: 'reply.reward_update', config: { rewardTitle: 'Hydrate', paused: 'keep', cooldown: 0 }, line: 'Turn off Hydrate cooldown' },
  { type: 'reply.countdown_stop', config: { name: 'subathon' }, line: 'Stop subathon' },
  { type: 'reply.cooldown_reset', config: { name: 'raffle' }, line: 'Reset raffle' }
])('$type renders "$line" from $config', ({ type, config, line, trigger }) => {
  expect(describe(english, { id: 'step', type, x: 0, y: 0, config }, trigger)).toBe(line);
});
