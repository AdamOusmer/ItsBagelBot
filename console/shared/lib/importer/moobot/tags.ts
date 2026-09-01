// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Tag catalog + renderers for Moobot responses — the port of tags.go. The
// parser core (./parse) hands each command's raw text here; everything
// tag-shaped lives in this file so the catalog can be reviewed against
// Moobot's insertable-tag list in one place.

// --- tag translation (ported from tags.go) ----------------------------------

import { fetchDefSlug } from '../validate';
import type { ManifestFetch } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';

// Moobot stores response tags literally as <name>; this exact character class
// is what their widget round-trips. Anything else in angle brackets is the
// broadcaster's literal text and survives untouched.
const TAG_PATTERN = /<([a-zA-Z0-9_.-]+)>/g;

export interface TagContext {
  name: string;
  randomStart?: number;
  randomEnd?: number;
  randomTexts: (TextOption[] | undefined)[];
  // fetchDefs is the import-level accumulator of synthesized urlfetch
  // definition shells, shared across every command in the export so slugs
  // dedupe by name (two commands normalizing onto one name share the shell).
  fetchDefs: Map<string, ManifestFetch>;
}

export interface TextOption {
  text: string;
}

// Full insertable-tag catalog of Moobot's custom-command editor (build r/453).
// Membership decides warn-vs-silent: a catalog entry we cannot map earns a
// warning, an unknown bracketed word does not.
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

// --- urlfetch mapping (docs/urlfetch/IMPLEMENTATION.md, Phase 4) -------------

// MOOBOT_JSON_SLOT recognizes the numbered urlfetch slots their editor inserts
// (urlfetch.json.1 .. urlfetch.json.10); urlfetch.plain is the un-numbered one.
const MOOBOT_JSON_SLOT = /^urlfetch\.json\.([1-9]|10)$/;

// FETCH_DEF_CAP reuses IMPORT_ITEM_CAPS.commands as the per-import ceiling on
// synthesized fetch definitions instead of minting a second public number:
// every definition exists only to serve a command in this same export, so the
// commands cap bounds it by construction, and one fewer magic number cannot
// drift from the mirrored server-side table. Past the cap the tag falls back
// to the literal+warn path — fail visible, never a dangling {urlfetch:}
// reference.
const FETCH_DEF_CAP = IMPORT_ITEM_CAPS.commands;

// urlfetchSlot classifies a tag: null for the un-numbered urlfetch.plain, the
// slot number for urlfetch.json.N, undefined for anything not urlfetch-shaped.
function urlfetchSlot(tag: string): number | null | undefined {
  if (tag === 'urlfetch.plain') return null;
  const m = MOOBOT_JSON_SLOT.exec(tag);
  return m ? Number(m[1]) : undefined;
}

// urlfetchRef maps one Moobot urlfetch tag onto its `{urlfetch:<slug>}`
// reference, synthesizing the definition shell on first sight. Slug rule:
// `fetchDefSlug('moobot', command.name)`, with the tag's own N appended for
// json.N — the TAG NAME is the slot id, so mapping is a pure function of the
// export and re-import lands on identical slugs (idempotent). Slots never
// merge even though none of them carries a URL: equality is unknowable until
// the broadcaster re-enters each URL, so plain and every json.N stay separate
// definitions forever. Returns null at the cap, which degrades to the
// literal+unmapped-warn path.
function urlfetchRef(ctx: TagContext, slotN: number | null): string | null {
  // Slug rule: fetchDefSlug keeps the name inside the commands service's
  // ^[a-z0-9_]{1,32}$ grammar (a hyphen is refused there), and the tag's own N
  // is appended for json.N — the TAG NAME is the slot id, so mapping is a pure
  // function of the export and re-import lands on identical slugs.
  const base = fetchDefSlug('moobot', ctx.name);
  const key = slotN === null ? base : `${base}_${slotN}`;
  if (!ctx.fetchDefs.has(key)) {
    if (ctx.fetchDefs.size >= FETCH_DEF_CAP) return null;
    // Deliberately NO url field: Moobot's BotCommand export carries no URL
    // data at all, so the shell is a placeholder the broadcaster must complete
    // in the fetch-definitions editor before anything can fetch.
    ctx.fetchDefs.set(key, { name: key, source: 'moobot' });
  }
  return key;
}

function randomNumberKey(ctx: TagContext): string {
  const { randomStart: s, randomEnd: e } = ctx;
  if (!usableRandomRange(s, e)) return '{random}';
  return `{random:${s}-${e}}`;
}

// usableRandomRange demands integral, ordered bounds inside int64; anything
// else takes the source-defined {random} fallback. Beyond 2^53 float64 cannot
// represent every integer anyway, so the int64-exactness Go prints there is
// unreachable from JSON inputs.
function usableRandomRange(s: number | undefined, e: number | undefined): boolean {
  if (s === undefined || e === undefined) return false;
  if (!Number.isInteger(s) || !Number.isInteger(e)) return false;
  if (s > e) return false;
  return s >= -(2 ** 63) && e <= 2 ** 63 - 1;
}

function choiceKey(opts: TextOption[] | undefined): string {
  if (!opts || opts.length === 0) return '';
  for (const o of opts) {
    if (typeof o?.text === 'string' && o.text.includes(',')) return '';
  }
  return '{choice:' + opts.map((o) => o.text).join(',') + '}';
}

// TAG_RENDERERS renders one insertable tag to its canonical replacement.
// Adding a tag later is a row here, not a branch in the translator.
const TAG_RENDERERS: Record<string, (ctx: TagContext) => string> = {
  username: () => '{user}',
  'twitch.mentioned': () => '{target}',
  args: () => '{args}',
  // Moobot's argument #1 falls back to the invoker's username when
  // absent — exactly the duality of our {target}. Arguments #2..#5 have
  // no clean equivalent ({args} would repeat the whole tail), so only #1
  // maps (decision record kept from tags.go).
  '1': () => '{target}',
  'random.number': randomNumberKey,
  // Moobot counters are per-command; ours are named channel-scope values,
  // keyed by the command's normalized name.
  counter: (ctx) => `{counter:${ctx.name}}`,
  'channel.name': () => '{channel}'
};
for (let i = 1; i <= 3; i++) TAG_RENDERERS[`random.text.${i}`] = (ctx) => choiceKey(ctx.randomTexts[i - 1]);

function replaceTag(tag: string, ctx: TagContext): string {
  const render = TAG_RENDERERS[tag];
  return render ? render(ctx) : '';
}

// FetchTagRef is one distinct urlfetch tag this response mapped, with the
// definition slug it landed on — the warn loop emits one "re-enter the URL"
// finding per entry without recomputing the slug rule.
export interface FetchTagRef {
  tag: string;
  key: string;
}

interface TagResult {
  text: string;
  unmapped: string[];
  counterUsed: boolean;
  fetchRefs: FetchTagRef[];
}

export function translateTags(text: string, ctx: TagContext): TagResult {
  const res: TagResult = { text: '', unmapped: [], counterUsed: false, fetchRefs: [] };
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

// TagRender bundles what one tag replacement may touch: the command context,
// the accumulating result and the once-per-tag bookkeeping. Grouped so the
// renderer takes a single value instead of parallel arguments.
interface TagRender {
  ctx: TagContext;
  res: TagResult;
  seen: Set<string>;
}

// renderTag contributes one tag's output: its replacement when mapped,
// otherwise the literal bracketed text plus a first-of-kind warning for
// catalog entries we cannot express (unknown bracketed words stay silent —
// they are indistinguishable from prose until Moobot defines them).
// The literal form is reconstructable from the name alone — TAG_PATTERN
// captures exactly the text between one "<" and one ">" — so renderers take
// the tag and rebuild the bracketed literal on the degrade paths.
function renderTag(render: TagRender, tag: string): string {
  const slot = urlfetchSlot(tag);
  if (slot !== undefined) return renderUrlfetchTag(render, tag, slot);
  const replacement = replaceTag(tag, render.ctx);
  if (replacement !== '') {
    if (tag === 'counter') render.res.counterUsed = true;
    return replacement;
  }
  noteUnmappedTag(render, tag);
  return `<${tag}>`;
}

// renderUrlfetchTag maps one urlfetch tag onto its {urlfetch:<slug>}
// reference. At the definition cap it takes the same literal+warn degrade as
// an unmappable catalog tag — never a dangling {urlfetch:} reference.
function renderUrlfetchTag(render: TagRender, tag: string, slot: number | null): string {
  const key = urlfetchRef(render.ctx, slot);
  if (key === null) {
    noteUnmappedTag(render, tag);
    return `<${tag}>`;
  }
  if (!render.res.fetchRefs.some((r) => r.tag === tag)) render.res.fetchRefs.push({ tag, key });
  return `{urlfetch:${key}}`;
}

// noteUnmappedTag records a first-of-kind warning for a known-but-unmappable
// catalog tag.
function noteUnmappedTag(render: TagRender, tag: string): void {
  if (!KNOWN_TAGS.has(tag) || render.seen.has(tag)) return;
  render.seen.add(tag);
  render.res.unmapped.push(tag);
}
