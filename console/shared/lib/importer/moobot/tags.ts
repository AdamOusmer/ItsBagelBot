// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Tag catalog + renderers for Moobot responses — the port of tags.go. The
// parser core (./parse) hands each command's raw text here; everything
// tag-shaped lives in this file so the catalog can be reviewed against
// Moobot's insertable-tag list in one place.

// --- tag translation (ported from tags.go) ----------------------------------

// Moobot stores response tags literally as <name>; this exact character class
// is what their widget round-trips. Anything else in angle brackets is the
// broadcaster's literal text and survives untouched.
const TAG_PATTERN = /<([a-zA-Z0-9_.-]+)>/g;

export interface TagContext {
  name: string;
  randomStart?: number;
  randomEnd?: number;
  randomTexts: (TextOption[] | undefined)[];
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

interface TagResult {
  text: string;
  unmapped: string[];
  counterUsed: boolean;
}

export function translateTags(text: string, ctx: TagContext): TagResult {
  const res: TagResult = { text: '', unmapped: [], counterUsed: false };
  const seen = new Set<string>();
  let out = '';
  let last = 0;
  for (const m of text.matchAll(TAG_PATTERN)) {
    const start = m.index ?? 0;
    out += text.slice(last, start);
    last = start + m[0].length;
    out += renderTag(m[0], m[1], ctx, res, seen);
  }
  out += text.slice(last);
  res.text = out;
  return res;
}

// renderTag contributes one tag's output: its replacement when mapped,
// otherwise the literal bracketed text plus a first-of-kind warning for
// catalog entries we cannot express (unknown bracketed words stay silent —
// they are indistinguishable from prose until Moobot defines them).
function renderTag(raw: string, tag: string, ctx: TagContext, res: TagResult, seen: Set<string>): string {
  const replacement = replaceTag(tag, ctx);
  if (replacement !== '') {
    if (tag === 'counter') res.counterUsed = true;
    return replacement;
  }
  if (KNOWN_TAGS.has(tag) && !seen.has(tag)) {
    seen.add(tag);
    res.unmapped.push(tag);
  }
  return raw;
}
