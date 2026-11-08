// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Chk } from './check';
import { register, type Table, type Wide } from './engine';
import { CODE } from './limits';
import type * as T from './stream-types';

function checkPoll(c: Chk, p: Wide<T.PollAction>): void {
  c.text('title', p.title, 1, 60);
  c.inRange('seconds', p.seconds, 15, 1800);
  c.stringList('choices', p.choices, 5, (f, s) => c.text(f, s, 1, 25));
  c.count('choices', p.choices.length, 2, 5);
}

function checkPrediction(c: Chk, p: Wide<T.PredictionAction>): void {
  c.text('title', p.title, 1, 45);
  c.inRange('seconds', p.seconds, 30, 1800);
  c.stringList('outcomes', p.outcomes, 10, (f, s) => c.text(f, s, 1, 25));
  c.count('outcomes', p.outcomes.length, 2, 10);
}

function checkPredictionEnd(c: Chk, p: Wide<T.PredictionEndAction>): void {
  c.oneOf('status', p.status, ['lock', 'cancel', 'resolve']);
  if (p.status !== 'resolve') return;
  if (p.winner === 0) c.add('winner', CODE.required);
  else c.inRange('winner', p.winner, 1, 10);
}

function checkUnbanResolve(c: Chk, p: Wide<T.UnbanResolveAction>): void {
  c.oneOf('status', p.status, ['approve', 'deny']);
  c.text('resolution', p.resolution, 0, 500);
}

function checkChatMode(c: Chk, p: Wide<T.ChatModeAction>): void {
  c.oneOf('mode', p.mode, ['emote_only', 'followers', 'slow', 'subscribers', 'unique']);
  c.inRange('value', p.value, 0, 86400);
}

function checkWhisper(c: Chk, p: Wide<T.WhisperAction>): void {
  c.login('login', p.login, 1);
  c.template('text', p.text);
}

const noop = () => {};
const login = register<{ login: string }>({ login: 'string' }, (c, p) => void c.login('login', p.login, 1));
const empty = register<Record<string, never>>({}, noop);
const loginToggle = register<Wide<T.LoginToggleAction>>({ login: 'string', on: 'bool' }, (c, p) => void c.login('login', p.login, 1));

export const STREAM_ACTIONS: Table = {
  whisper: register<Wide<T.WhisperAction>>({ login: 'string', text: 'string' }, checkWhisper),
  poll: register<Wide<T.PollAction>>({ title: 'string', choices: 'strings', seconds: 'int' }, checkPoll),
  poll_end: register<Wide<T.PollEndAction>>({ status: 'string' }, (c, p) => c.oneOf('status', p.status, ['terminate', 'archive'])),
  prediction: register<Wide<T.PredictionAction>>({ title: 'string', outcomes: 'strings', seconds: 'int' }, checkPrediction),
  prediction_end: register<Wide<T.PredictionEndAction>>({ status: 'string', winner: 'int' }, checkPredictionEnd),
  raid: login,
  raid_cancel: empty,
  mod: loginToggle,
  vip: loginToggle,
  clear_chat: empty,
  ad_snooze: empty,
  chat_mode: register<Wide<T.ChatModeAction>>({ mode: 'string', on: 'bool', value: 'int' }, checkChatMode),
  unban_resolve: register<Wide<T.UnbanResolveAction>>({ status: 'string', resolution: 'string' }, checkUnbanResolve),
  branded_content: register<Wide<T.BrandedContentAction>>({ on: 'bool' }, noop),
};
