// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { intactSpan } from '../engine/tmpl';
import { intactSpanWithFallback } from '../engine/tmpl-fallback';
import { isRFC3339, validCalendarDay, validClock } from './validate';

export type Concept =
  | 'user'
  | 'touser'
  | 'args'
  | 'querystring'
  | 'channel'
  | 'title'
  | 'game'
  | 'uptime'
  | 'channel.viewers'
  | 'followers'
  | 'subs'
  | 'followage'
  | 'accountage'
  | 'random'
  | 'choice'
  | 'random.viewer'
  | 'count'
  | 'counter'
  | 'countdown'
  | 'countup'
  | 'time'
  | 'urlfetch'
  | 'points'
  | 'points.name'
  | 'if'
  | 'math'
  | 'repeat'
  | 'chatters'
  | 'user.id'
  | 'user.login';

type WordIndex = number;
type RawText = string;

export const TARGETS: Record<Concept, string> = {
  user: 'user',
  touser: 'touser',
  args: 'args',
  querystring: 'querystring',
  channel: 'channel',
  title: 'title',
  game: 'game',
  uptime: 'uptime',
  'channel.viewers': 'channel.viewers',
  followers: 'followers',
  subs: 'subs',
  followage: 'followage',
  accountage: 'accountage',
  random: 'random',
  choice: 'choice',
  'random.viewer': 'random.viewer',
  count: 'count',
  counter: 'counter',
  countdown: 'countdown',
  countup: 'countup',
  time: 'time',
  urlfetch: 'urlfetch',
  points: 'points',
  'points.name': 'points.name',
  if: 'if',
  math: 'math',
  repeat: 'repeat',
  chatters: 'chatters',
  'user.id': 'user.id',
  'user.login': 'user.login'
};

export function emit(concept: Concept, payload: RawText | null = null): string | null {
  return intactSpan(TARGETS[concept], payload);
}

export const POSITIONAL_MAX = 30;

function isPositionalIndex(x: WordIndex): boolean {
  return Number.isInteger(x) && x >= 1 && x <= POSITIONAL_MAX;
}

export function positional(n: WordIndex, fallback?: RawText): string | null {
  if (!isPositionalIndex(n)) return null;
  return fallback === undefined ? intactSpan(String(n), null) : intactSpanWithFallback(String(n), null, fallback);
}

function slicedBoundInvalid(bound: WordIndex | undefined): boolean {
  return bound !== undefined && !isPositionalIndex(bound);
}

export function slice(n?: WordIndex, m?: WordIndex, fallback?: RawText): string | null {
  if (n === undefined && m === undefined) return null;
  if (slicedBoundInvalid(n) || slicedBoundInvalid(m)) return null;
  const name = n === undefined ? '' : String(n);
  const payload = m === undefined ? '' : String(m);
  return fallback === undefined ? intactSpan(name, payload) : intactSpanWithFallback(name, payload, fallback);
}

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})$/;

export const TIME_ONLY = /^\d{1,2}:\d{2}(:\d{2})?\s*(am|pm)?\s*[a-z0-9:+-]*$/i;

const MONTH_NUMBER: Record<string, number> = {
  jan: 1,
  feb: 2,
  mar: 3,
  apr: 4,
  may: 5,
  jun: 6,
  jul: 7,
  aug: 8,
  sep: 9,
  oct: 10,
  nov: 11,
  dec: 12
};

const ZONE_OFFSETS_MIN: Record<string, number> = {
  utc: 0,
  gmt: 0,
  z: 0,
  est: -5 * 60,
  edt: -4 * 60,
  cst: -6 * 60,
  cdt: -5 * 60,
  mst: -7 * 60,
  mdt: -6 * 60,
  pst: -8 * 60,
  pdt: -7 * 60
};

function zoneOffsetMinutes(zone: RawText): number | null {
  const named = ZONE_OFFSETS_MIN[zone.toLowerCase()];
  if (named !== undefined) return named;
  const m = /^([+-])(\d{2}):?(\d{2})$/.exec(zone);
  if (!m) return null;
  const sign = m[1] === '-' ? -1 : 1;
  return sign * (Number(m[2]) * 60 + Number(m[3]));
}

const MONTH_NAME_DATE =
  /^([A-Za-z]{3})\s+(\d{1,2}),?\s+(\d{4})(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp][Mm])?)?\s+(\S+)$/;
const NUMERIC_DATE =
  /^(\d{1,2})\/(\d{1,2})\/(\d{4})(?:\s+(\d{1,2}):(\d{2})(?::(\d{2}))?\s*([AaPp][Mm])?)?\s+(\S+)$/;

function hourFromClock(clock: { h?: string; ampm?: string }): number {
  const hour = clock.h === undefined ? 0 : Number(clock.h);
  if (!clock.ampm) return hour;
  const isPM = clock.ampm.toLowerCase() === 'pm';
  if (hour === 12) return isPM ? 12 : 0;
  return isPM ? hour + 12 : hour;
}

function dateTimeToInstant(
  day: { y: number; mo: number; d: number },
  clock: { h?: string; mi?: string; s?: string; ampm?: string },
  zone: RawText
): string | null {
  if (!validCalendarDay(day)) return null;
  const hour = hourFromClock(clock);
  const minute = clock.mi === undefined ? 0 : Number(clock.mi);
  const second = clock.s === undefined ? 0 : Number(clock.s);
  if (!validClock({ h: hour, mi: minute, s: second })) return null;
  const offsetMin = zoneOffsetMinutes(zone);
  if (offsetMin === null) return null;
  const utcMs = Date.UTC(day.y, day.mo - 1, day.d, hour, minute, second) - offsetMin * 60000;
  return new Date(utcMs).toISOString();
}

type DateForm = (trimmed: RawText) => string | null | undefined;

function rfc3339Form(trimmed: RawText): string | null | undefined {
  return isRFC3339(trimmed) ? trimmed : undefined;
}

function dateOnlyForm(trimmed: RawText): string | null | undefined {
  const m = DATE_ONLY.exec(trimmed);
  if (!m) return undefined;
  const day = { y: Number(m[1]), mo: Number(m[2]), d: Number(m[3]) };
  return validCalendarDay(day) ? trimmed : null;
}

function timeOnlyForm(trimmed: RawText): string | null | undefined {
  return TIME_ONLY.test(trimmed) ? null : undefined;
}

function monthNameForm(trimmed: RawText): string | null | undefined {
  const m = MONTH_NAME_DATE.exec(trimmed);
  if (!m) return undefined;
  const mo = MONTH_NUMBER[m[1].toLowerCase()];
  if (mo === undefined) return null;
  const day = { y: Number(m[3]), mo, d: Number(m[2]) };
  return dateTimeToInstant(day, { h: m[4], mi: m[5], s: m[6], ampm: m[7] }, m[8]);
}

function numericForm(trimmed: RawText): string | null | undefined {
  const m = NUMERIC_DATE.exec(trimmed);
  if (!m) return undefined;
  const day = { y: Number(m[3]), mo: Number(m[1]), d: Number(m[2]) };
  return dateTimeToInstant(day, { h: m[4], mi: m[5], s: m[6], ampm: m[7] }, m[8]);
}

const DATE_FORMS: DateForm[] = [rfc3339Form, dateOnlyForm, timeOnlyForm, monthNameForm, numericForm];

export function normalizeInstant(payload: RawText): string | null {
  const trimmed = payload.trim();
  for (const form of DATE_FORMS) {
    const result = form(trimmed);
    if (result !== undefined) return result;
  }
  return null;
}
