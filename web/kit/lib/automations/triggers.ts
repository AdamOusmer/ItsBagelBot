// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Chk } from './check';
import { register, type Wide, type Table } from './engine';
import { lookupEventKind, type EventKind } from './event-kinds';
import { CODE, MAX_COMMAND_ALIASES, MAX_EVENT_FILTERS, MAX_FILTER_VALUE, MAX_PATTERN_LENGTH } from './limits';
import { compilesAsRe2 } from './regex';
import { compareBytes, isCommandChar, isPrintChar, parsesAsInt64 } from './text';
import type { CommandTrigger, EventTrigger, RewardTrigger, TimerTrigger } from './types';

const MATCH_MODES = ['start', 'exact', 'anywhere', 'regex'];
const COMMAND_SOURCES = ['chat', 'sub_message'];
const REGEX_MATCH = 'regex';
const MAX_COMMAND_NAME = 64;

type Command = Wide<CommandTrigger>;

export function checkRegex(c: Chk, field: string, s: string, hi: number): void {
  if (!c.text(field, s, 1, hi)) return;
  if (!compilesAsRe2(s)) c.add(field, CODE.invalidRegex);
}

export function commandName(c: Chk, field: string, s: string, match: string): void {
  if (match === REGEX_MATCH) {
    checkRegex(c, field, s, MAX_PATTERN_LENGTH);
    return;
  }
  if (c.text(field, s, 1, MAX_COMMAND_NAME)) c.chars(field, s, match === 'anywhere' ? isPrintChar : isCommandChar);
}

function checkAliases(c: Chk, p: Command): void {
  if (p.match === REGEX_MATCH && p.aliases.length > 0) {
    c.add('aliases', CODE.invalidFormat);
    return;
  }
  if (!c.count('aliases', p.aliases.length, 0, MAX_COMMAND_ALIASES)) return;
  p.aliases.forEach((s, i) => commandName(c, `aliases[${i}]`, s, p.match));
}

function checkCommand(c: Chk, p: Command): void {
  c.oneOf('match', p.match, MATCH_MODES);
  commandName(c, 'name', p.name, p.match);
  checkAliases(c, p);
  if (!c.count('sources', p.sources.length, 1, COMMAND_SOURCES.length)) return;
  p.sources.forEach((s, i) => c.oneOf(`sources[${i}]`, s, COMMAND_SOURCES));
}

function checkFilter(c: Chk, kind: EventKind | undefined, key: string, value: string): void {
  const field = `filters.${key}`;
  c.text(field, value, 0, MAX_FILTER_VALUE);
  if (!kind) return;
  const f = kind.fields.find((x) => x.name === key);
  if (!f) c.add(field, CODE.unknownFilter);
  else if (f.numeric && !parsesAsInt64(value)) c.add(field, CODE.invalidFormat);
}

function checkEvent(c: Chk, p: Wide<EventTrigger>): void {
  const kind = lookupEventKind(p.kind);
  if (!kind) c.add('kind', p.kind === '' ? CODE.required : CODE.unknownEventKind);
  c.count('filters', Object.keys(p.filters).length, 0, MAX_EVENT_FILTERS);
  Object.keys(p.filters)
    .sort(compareBytes)
    .forEach((key) => checkFilter(c, kind, key, p.filters[key]));
}

function checkTimer(c: Chk, p: Wide<TimerTrigger>): void {
  c.inRange('interval_s', p.interval_s, 60, 86400);
  c.inRange('random_jitter_s', p.random_jitter_s, 0, 3600);
  c.inRange('min_chat_lines', p.min_chat_lines, 0, 100);
}

function checkReward(c: Chk, p: Wide<RewardTrigger>): void {
  c.text('reward_id', p.reward_id, 1, 64);
}

export const TRIGGERS: Table = {
  command: register<Command>(
    { name: 'string', aliases: 'strings', match: 'string', case_sensitive: 'bool', sources: 'strings' },
    checkCommand,
    { match: 'start', sources: ['chat'] },
  ),
  event: register<Wide<EventTrigger>>({ kind: 'string', filters: 'strmap' }, checkEvent),
  timer: register<Wide<TimerTrigger>>(
    { interval_s: 'int', random_jitter_s: 'int', min_chat_lines: 'int', offline_ok: 'bool' },
    checkTimer,
  ),
  reward: register<Wide<RewardTrigger>>({ reward_id: 'string' }, checkReward),
  manual: register<Record<string, never>>({}, () => {}),
};
