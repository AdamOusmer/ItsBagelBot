// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fetchDefSlug } from '../validate';
import { emit, positional } from '../targets';
import type { ManifestFetch } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';

const TAG_PATTERN = /<([a-zA-Z0-9_.-]+)>/g;

export interface TagContext {
  name: string;
  randomStart?: number;
  randomEnd?: number;
  randomTexts: (TextOption[] | undefined)[];
  fetchDefs: Map<string, ManifestFetch>;
}

export interface TextOption {
  text: string;
}

const KNOWN_TAGS = new Set<string>([
  'text', 'username', 'twitch.mentioned', 'by', 'counter', 'when',
  'random.number', 'args', 'args.url', 'channel.name', 'channel.name.sc',
  'urlfetch.plain', 'countdown', 'countdown.time', 'countup', 'time',
  'uptime', 'uptime.timestamp', 'random.userlist', 'lastfm.current',
  'twitch.title', 'twitch.game', 'twitch.followers', 'twitch.viewers',
  'twitch.followed', 'twitch.subs.count', 'twitch.subs.score',
  'twitch.subs.latest', 'twitch.subs.latest.when',
  'youtube.title', 'youtube.url', 'youtube.views', 'youtube.ago',
  'lol.league', 'lol.points',
  'tft.league', 'tft.points', 'tft.wins', 'tft.losses', 'tft.winrate',
  'apex.rank', 'apex.legend', 'apex.level', 'apex.kills',
  'apex.kills.current', 'apex.damage'
]);
for (let i = 1; i <= 5; i++) KNOWN_TAGS.add(String(i));
for (let i = 1; i <= 3; i++) KNOWN_TAGS.add(`random.text.${i}`);
for (let i = 1; i <= 10; i++) KNOWN_TAGS.add(`urlfetch.json.${i}`);

const MOOBOT_JSON_SLOT = /^urlfetch\.json\.([1-9]|10)$/;

const FETCH_DEF_CAP = IMPORT_ITEM_CAPS.commands;

function urlfetchSlot(tag: string): number | null | undefined {
  if (tag === 'urlfetch.plain') return null;
  const m = MOOBOT_JSON_SLOT.exec(tag);
  return m ? Number(m[1]) : undefined;
}

function urlfetchRef(ctx: TagContext, slotN: number | null): string | null {
  const base = fetchDefSlug('moobot', ctx.name);
  const key = slotN === null ? base : `${base}_${slotN}`;
  if (!ctx.fetchDefs.has(key)) {
    if (ctx.fetchDefs.size >= FETCH_DEF_CAP) return null;
    ctx.fetchDefs.set(key, { name: key, source: 'moobot' });
  }
  return key;
}

function randomNumberKey(ctx: TagContext): string {
  const { randomStart: s, randomEnd: e } = ctx;
  if (!usableRandomRange(s, e)) return emit('random') ?? '';
  return emit('random', `${s}-${e}`) ?? emit('random') ?? '';
}

function usableRandomRange(s: number | undefined, e: number | undefined): boolean {
  if (s === undefined || e === undefined) return false;
  if (!Number.isInteger(s) || !Number.isInteger(e)) return false;
  if (s > e) return false;
  return s >= -(2 ** 63) && e <= 2 ** 63 - 1;
}

const SPAN_BYTES = /[|}]/g;

function choiceKey(opts: TextOption[] | undefined): string {
  if (!opts || opts.length === 0) return '';
  for (const o of opts) {
    if (typeof o?.text === 'string' && o.text.includes(',')) return '';
  }
  return emit('choice', opts.map(optionText).join(',')) ?? '';
}

function optionText(o: TextOption): string {
  return typeof o?.text === 'string' ? o.text.replace(SPAN_BYTES, '') : '';
}

const TAG_RENDERERS: Record<string, (ctx: TagContext) => string> = {
  username: () => emit('user') ?? '',
  'twitch.mentioned': () => emit('touser') ?? '',
  args: () => emit('args') ?? '',
  '1': () => emit('touser') ?? '',
  'random.number': randomNumberKey,
  counter: () => emit('count') ?? '',
  'channel.name': () => emit('channel') ?? '',
  uptime: () => emit('uptime') ?? '',
  'twitch.title': () => emit('title') ?? '',
  'twitch.game': () => emit('game') ?? '',
  'twitch.viewers': () => emit('channel.viewers') ?? '',
  'twitch.followed': () => emit('followage') ?? '',
  'twitch.followers': () => emit('followers') ?? '',
  'twitch.subs.count': () => emit('subs') ?? '',
  'random.userlist': () => emit('random.viewer') ?? '',
  time: () => emit('time') ?? '',
  'args.url': () => emit('querystring') ?? ''
};
for (let i = 1; i <= 3; i++) TAG_RENDERERS[`random.text.${i}`] = (ctx) => choiceKey(ctx.randomTexts[i - 1]);
for (let i = 2; i <= 5; i++) TAG_RENDERERS[String(i)] = () => positional(i) ?? '';

function replaceTag(tag: string, ctx: TagContext): string {
  const render = TAG_RENDERERS[tag];
  return render ? render(ctx) : '';
}

export interface FetchTagRef {
  tag: string;
  key: string;
}

export interface TagResult {
  text: string;
  unmapped: string[];
  unrecognized: string[];
  fetchRefs: FetchTagRef[];
  countRemapped: boolean;
  positionalFallbackLost: string[];
}

export function translateTags(text: string, ctx: TagContext): TagResult {
  const res: TagResult = {
    text: '',
    unmapped: [],
    unrecognized: [],
    fetchRefs: [],
    countRemapped: false,
    positionalFallbackLost: []
  };
  const seen = new Set<string>();
  const render: TagRender = { ctx, res, seen };
  let out = '';
  let last = 0;
  for (const m of text.matchAll(TAG_PATTERN)) {
    const start = m.index ?? 0;
    out += text.slice(last, start);
    last = start + m[0].length;
    out += renderTag(render, m[1]);
  }
  out += text.slice(last);
  res.text = out;
  return res;
}

interface TagRender {
  ctx: TagContext;
  res: TagResult;
  seen: Set<string>;
}

const LOST_FALLBACK_ARGS = new Set(['2', '3', '4', '5']);

function renderTag(render: TagRender, tag: string): string {
  const slot = urlfetchSlot(tag);
  if (slot !== undefined) return renderUrlfetchTag(render, tag, slot);
  const replacement = replaceTag(tag, render.ctx);
  if (replacement !== '') {
    if (tag === 'counter') render.res.countRemapped = true;
    if (LOST_FALLBACK_ARGS.has(tag)) render.res.positionalFallbackLost.push(tag);
    return replacement;
  }
  noteUnmappedTag(render, tag);
  return `<${tag}>`;
}

function renderUrlfetchTag(render: TagRender, tag: string, slot: number | null): string {
  const key = urlfetchRef(render.ctx, slot);
  if (key === null) {
    noteUnmappedTag(render, tag);
    return `<${tag}>`;
  }
  const span = emit('urlfetch', key);
  if (span === null) {
    noteUnmappedTag(render, tag);
    return `<${tag}>`;
  }
  if (!render.res.fetchRefs.some((r) => r.tag === tag)) render.res.fetchRefs.push({ tag, key });
  return span;
}

function noteUnmappedTag(render: TagRender, tag: string): void {
  if (render.seen.has(tag)) return;
  render.seen.add(tag);
  (KNOWN_TAGS.has(tag) ? render.res.unmapped : render.res.unrecognized).push(tag);
}
