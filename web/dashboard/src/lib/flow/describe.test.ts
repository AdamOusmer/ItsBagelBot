// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
import { expect, test } from 'bun:test';
import { describe } from './describe';
import { english } from './english.testing';
import type { FlowNode } from './model';

test.each<{ type: FlowNode['type']; config: FlowNode['config']; line: string }>([
  { type: 'countdown.finished', config: { name: 'subathon' }, line: 'When subathon reaches zero' },
  { type: 'reply.chat', config: { message: 'Hello {user}', reply: 'thread' }, line: 'Hello {user}' },
  { type: 'reply.poll', config: { title: 'Next game?' }, line: 'Poll: Next game?' },
  { type: 'reply.poll', config: { title: '  ' }, line: 'Let viewers vote on a question' },
  { type: 'reply.prediction', config: { title: 'Will we win?' }, line: 'Prediction: Will we win?' },
  { type: 'reply.poll_end', config: { status: 'terminate' }, line: 'End poll and show results' },
  { type: 'reply.poll_end', config: { status: 'archive' }, line: 'End poll and hide results' },
  { type: 'reply.poll_end', config: { status: '' }, line: 'Stop voting or archive the results' },
  { type: 'reply.prediction_end', config: { status: 'lock' }, line: 'Lock bets' },
  { type: 'reply.prediction_end', config: { status: 'cancel' }, line: 'Cancel and refund' },
  { type: 'reply.prediction_end', config: { status: 'resolve', outcome: 'Team Blue' }, line: 'Winner: Team Blue' },
  { type: 'reply.prediction_end', config: { status: 'resolve' }, line: 'Lock bets, refund points or pick a winner' },
  { type: 'reply.raid', config: { target: 'lotti' }, line: 'Raid lotti' },
  { type: 'reply.mod', config: { target: 'lotti', on: true }, line: 'Make lotti a mod' },
  { type: 'reply.mod', config: { target: 'lotti', on: false }, line: 'Remove mod from lotti' },
  { type: 'reply.mod', config: { on: true }, line: 'Give or remove moderator access' },
  { type: 'reply.vip', config: { target: 'lotti', on: true }, line: 'Make lotti a VIP' },
  { type: 'reply.vip', config: { target: 'lotti', on: false }, line: 'Remove VIP from lotti' },
  { type: 'reply.chat_mode', config: { mode: 'slow', on: true }, line: 'Slow mode on' },
  { type: 'reply.chat_mode', config: { mode: 'slow', on: true, wait: 30 }, line: 'Slow mode on, 30 s' },
  { type: 'reply.chat_mode', config: { mode: 'chat_delay', on: true, delay: '4' }, line: 'Chat delay on' },
  { type: 'reply.chat_mode', config: { mode: 'followers', on: false }, line: 'Followers only off' },
  { type: 'reply.chat_mode', config: { mode: '', on: true }, line: 'Change who can chat and how often' },
  { type: 'reply.whisper', config: { target: '{user}', text: 'Hi {target}' }, line: 'Whisper {user}: Hi {target}' },
  { type: 'reply.whisper', config: { target: 'lotti', text: '' }, line: 'Send a private message to one viewer' },
  { type: 'reply.clear_chat', config: {}, line: 'Clear all chat' },
  { type: 'reply.delete_message', config: {}, line: 'Delete the trigger message' },
  { type: 'reply.pin', config: { text: 'Read the rules' }, line: 'Pin: Read the rules' },
  { type: 'reply.pin', config: { text: '' }, line: 'Post a message and pin it' },
  { type: 'reply.countdown_start', config: { name: 'subathon', seconds: 3660 }, line: 'Start subathon for 1h 1m' },
  { type: 'reply.countdown_start', config: { name: 'subathon', seconds: '' }, line: 'Start or restart a named countdown' },
  { type: 'reply.countdown_add', config: { name: 'subathon', seconds: 90 }, line: 'Add 1m 30s to subathon' },
  { type: 'reply.cooldown_add', config: { name: 'raffle', seconds: 60 }, line: 'Add 1m to raffle' },
  { type: 'reply.shoutout', config: { target: 'lotti' }, line: 'Shoutout to lotti' }
])('$type renders "$line" from $config', ({ type, config, line }) => {
  expect(describe(english, { id: 'step', type, x: 0, y: 0, config })).toBe(line);
});
