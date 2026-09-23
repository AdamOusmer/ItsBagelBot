// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Tag catalog + renderers for Moobot responses, the port of tags.go. The
// parser core (./parse) hands each command's raw text here; everything
// tag-shaped lives in this file so the catalog can be reviewed against
// Moobot's insertable-tag list in one place.

// --- tag translation (ported from tags.go) ----------------------------------

import { fetchDefSlug } from '../validate';
import { emit, positional } from '../targets';
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
// No longer gates warn-vs-silent (phase 6): EVERY bracketed word this bot
// cannot map now warns, catalog member or not — renderTag's unrecognized
// case below reads a different message off it than the known-but-unmappable
// case, so the broadcaster is told WHICH kind of miss they are looking at.
// The old fully-silent path for a non-member read a stray "<sad>" in a
// sentence as indistinguishable from an unmapped tag and said nothing either
// way; a warn costs nothing for genuine prose, since review already shows
// the untranslated text unchanged.
const KNOWN_TAGS = new Set<string>([
  'text', 'username', 'twitch.mentioned', 'by', 'counter', 'when',
  'random.number', 'args', 'args.url', 'channel.name', 'channel.name.sc',
  'urlfetch.plain', 'countdown', 'countdown.time', 'countup', 'time',
  'uptime', 'uptime.timestamp', 'random.userlist', 'lastfm.current',
  'twitch.title', 'twitch.game', 'twitch.followers', 'twitch.viewers',
  'twitch.followed', 'twitch.subs.count', 'twitch.subs.score',
  'twitch.subs.latest', 'twitch.subs.latest.when',
  // youtube.*/lol.*/tft.*/apex.* (below): third-party stats have no resolver
  // here — this bot answers Twitch facts, not League/TFT/Apex ranks or a
  // linked YouTube channel's numbers, so every one of these stays on the
  // known-but-unmappable warn path with nothing to map onto, ever.
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
// to the literal+warn path: fail visible, never a dangling {urlfetch:}
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
// json.N: the TAG NAME is the slot id, so mapping is a pure function of the
// export and re-import lands on identical slugs (idempotent). Slots never
// merge even though none of them carries a URL: equality is unknowable until
// the broadcaster re-enters each URL, so plain and every json.N stay separate
// definitions forever. Returns null at the cap, which degrades to the
// literal+unmapped-warn path.
function urlfetchRef(ctx: TagContext, slotN: number | null): string | null {
  // Slug rule: fetchDefSlug keeps the name inside the commands service's
  // ^[a-z0-9_]{1,32}$ grammar (a hyphen is refused there), and the tag's own N
  // is appended for json.N: the TAG NAME is the slot id, so mapping is a pure
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
  if (!usableRandomRange(s, e)) return emit('random') ?? '';
  return emit('random', `${s}-${e}`) ?? emit('random') ?? '';
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

// SPAN_BYTES are the two bytes an option may not carry into a {choice:…}
// payload: '}' closes the span at the first one, amputating every option after
// it, and '|' re-reads the tail as the span's fallback.
const SPAN_BYTES = /[|}]/g;

// choiceKey renders one Moobot random-text list as {choice:a,b,c}.
//
// Decision record: a comma REFUSES the tag (returns '', the source-defined
// no-op) while the two span bytes are STRIPPED, because the two damages differ
// in kind. A comma is {choice}'s own option separator, so an option carrying
// one silently turns three options into four and changes what the command can
// say; there is no repair that preserves the author's intent, so the tag is
// dropped. '|' and '}' carry no meaning inside an option — they only end the
// span early — so removing the byte keeps every option and its order and
// costs one piece of punctuation, which is strictly better than importing a
// command that says half of what it used to.
//
// The result is round-tripped through intactSpan rather than trusted: if the
// grammar ever spends a third byte, this emits nothing instead of emitting a
// token that resolves to something the review screen never showed.
function choiceKey(opts: TextOption[] | undefined): string {
  if (!opts || opts.length === 0) return '';
  for (const o of opts) {
    if (typeof o?.text === 'string' && o.text.includes(',')) return '';
  }
  return emit('choice', opts.map(optionText).join(',')) ?? '';
}

// optionText is one option's payload text: non-string bodies read as empty,
// matching what Array.join already did with them.
function optionText(o: TextOption): string {
  return typeof o?.text === 'string' ? o.text.replace(SPAN_BYTES, '') : '';
}

// TAG_RENDERERS renders one insertable tag to its canonical replacement.
// Adding a tag later is a row here, not a branch in the translator.
const TAG_RENDERERS: Record<string, (ctx: TagContext) => string> = {
  username: () => emit('user') ?? '',
  'twitch.mentioned': () => emit('touser') ?? '',
  args: () => emit('args') ?? '',
  // Moobot's argument #1 falls back to the invoker's username when absent —
  // exactly {touser}'s own duality (sender when nobody was named). Arguments
  // #2..#5 carry no such fallback (a missing word is just empty), so they map
  // onto the plain positional words {2}..{5} instead and lose only the
  // username-fallback behaviour, noted with a warn at the call site below —
  // strictly better than the old silent-drop path, which produced no token
  // at all for a tag broadcasters actually use.
  '1': () => emit('touser') ?? '',
  'random.number': randomNumberKey,
  // Moobot's <counter> is per-command and auto-increments on every run,
  // exactly what {count} (the {uses} alias, uses.go) already is here — not a
  // named channel counter. Mapping it onto {counter:<name>} used to invent a
  // name from the command and silently bind the import to a channel counter
  // the broadcaster never created; {count} needs no name and carries no
  // payload, so the command context (ctx.name) plays no part in it any more.
  counter: () => emit('count') ?? '',
  'channel.name': () => emit('channel') ?? '',
  // Bare, argument-less channel facts: Moobot's own catalog lists each as its
  // own tag rather than a format string, so — unlike Nightbot's
  // $(twitch … "{{…}}") — there is a real variable to map onto here.
  uptime: () => emit('uptime') ?? '',
  'twitch.title': () => emit('title') ?? '',
  'twitch.game': () => emit('game') ?? '',
  'twitch.viewers': () => emit('channel.viewers') ?? '',
  'twitch.followed': () => emit('followage') ?? '',
  'twitch.followers': () => emit('followers') ?? '',
  // twitch.subs.count is the running subscriber count; .score/.latest/
  // .latest.when have no counterpart (a leaderboard rank and a most-recent-
  // subscriber name/time, neither of which this bot tracks) and stay on the
  // known-but-unmapped warn path.
  'twitch.subs.count': () => emit('subs') ?? '',
  // <random.userlist> draws one name out of the people currently in chat —
  // {random.viewer}'s own definition, not the same list {random.chatter}
  // (recent talkers) draws from.
  'random.userlist': () => emit('random.viewer') ?? '',
  time: () => emit('time') ?? '',
  // <args.url> is Moobot's URL-encoded argument tail: the same reason
  // {querystring} exists apart from {args} in every other source's table
  // (nightbot/variables.ts's decision record on $(querystring) applies here
  // unchanged).
  'args.url': () => emit('querystring') ?? ''
};
for (let i = 1; i <= 3; i++) TAG_RENDERERS[`random.text.${i}`] = (ctx) => choiceKey(ctx.randomTexts[i - 1]);
// Arguments #2..#5: see the decision record on '1' above.
for (let i = 2; i <= 5; i++) TAG_RENDERERS[String(i)] = () => positional(i) ?? '';

function replaceTag(tag: string, ctx: TagContext): string {
  const render = TAG_RENDERERS[tag];
  return render ? render(ctx) : '';
}

// FetchTagRef is one distinct urlfetch tag this response mapped, with the
// definition slug it landed on: the warn loop emits one "re-enter the URL"
// finding per entry without recomputing the slug rule.
export interface FetchTagRef {
  tag: string;
  key: string;
}

export interface TagResult {
  text: string;
  unmapped: string[];
  // unrecognized lists, first-seen order, every bracketed word this response
  // used that is not in Moobot's own catalog at all — distinct from
  // `unmapped` (a real catalog tag with no equivalent here) so the caller can
  // warn with a different message: "not a Moobot tag" reads very differently
  // from "Moobot has this, we don't".
  unrecognized: string[];
  fetchRefs: FetchTagRef[];
  // countRemapped is true when the response used <counter> at least once.
  // <counter> now maps onto {count} (a per-command use count, not a named
  // channel counter — see the TAG_RENDERERS entry above), which is a
  // meaning change for anyone who read the old value in chat, so
  // parse.ts's caller uses this to warn once per command rather than
  // silently changing what the response says.
  countRemapped: boolean;
  // positionalFallbackLost lists, first-seen order, every <2>..<5> tag this
  // response used: each mapped onto its plain positional word ({2}..{5}),
  // but Moobot's own falls back to the invoker's username when that word is
  // absent and this bot's positional words fall back to empty — a shift the
  // broadcaster cannot see just by reading the translated response.
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
// catalog entries we cannot express (unknown bracketed words stay silent:
// they are indistinguishable from prose until Moobot defines them).
// The literal form is reconstructable from the name alone (TAG_PATTERN
// captures exactly the text between one "<" and one ">") so renderers take
// the tag and rebuild the bracketed literal on the degrade paths.
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

// renderUrlfetchTag maps one urlfetch tag onto its {urlfetch:<slug>}
// reference. At the definition cap it takes the same literal+warn degrade as
// an unmappable catalog tag, never a dangling {urlfetch:} reference.
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

// noteUnmappedTag records a first-of-kind warning for one bracketed word this
// response could not translate — a known catalog tag with no equivalent, or
// (phase 6) a word that is not one of Moobot's tags at all. seen dedupes
// across BOTH lists together, so a tag never earns two different warnings.
function noteUnmappedTag(render: TagRender, tag: string): void {
  if (render.seen.has(tag)) return;
  render.seen.add(tag);
  (KNOWN_TAGS.has(tag) ? render.res.unmapped : render.res.unrecognized).push(tag);
}
