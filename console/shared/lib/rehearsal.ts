// Chat-rehearsal core: the dashboard-side mirror of how the bot expands and
// routes a response template. Every rule here corresponds to one place in the
// Go engine — keep them in lockstep:
//
//   - token expansion:       app/sesame/module/vars.go (Expand, ParseDynamic)
//   - command tokens:        app/sesame/engine/vars.go (expandCommand)
//   - counter normalization: app/sesame/engine/loyalty_valkey.go (NormalizeCounterName)
//   - slash-verb routing:    app/sesame/engine/slash.go (Translate)
//   - emit order + line cap: app/sesame/engine/dispatch.go (emitResponse)
//
// The bot has exactly two expansion behaviors, so there are exactly two
// rehearsals:
//
//   rehearseCommand — custom "!command" responses. The engine expands the
//   whole template first, then splits it into lines (cap 5, one chat message
//   each), then translates a leading slash-verb per line into a native Twitch
//   action (/announce, /shoutout, /pin, /me).
//
//   rehearseReply — module replies (alerts, trigger words, channel-point
//   rewards, built-ins, gateway commands). Each module expands ONLY its own
//   token map — most fall back to the shared dynamic tokens ({random},
//   {choice:…}), a few (govee, clip) do not — and emits one plain chat
//   message. Reply surfaces never run Translate: a literal "/announce hi"
//   is sent as plain text, and the rehearsal shows it that way.

import { RESPONSE_MAX_LINES, responseLines } from './commands-validate';

export type SegKind = 'plain' | 'sample' | 'unknown';

/** One run of rehearsed text: literal, a substituted sample, or a token the
 * bot would leave untouched (kept literal so typos stay visible). */
export interface Seg {
  text: string;
  kind: SegKind;
}

export type RehearsalMode = 'chat' | 'announce' | 'shoutout' | 'pin' | 'me';

/** One chat message as the bot would send it. */
export interface RehearsedLine {
  mode: RehearsalMode;
  /** The matched slash verb, e.g. "/announcegreen" (command rehearsal only). */
  verb?: string;
  /** Announce accent color key ("primary" for the bare verb). */
  color?: string;
  /** Shoutout target with the leading '@' removed. */
  target?: string;
  segments: Seg[];
}

/** Sample values for exactly the tokens expandCommand resolves — nothing
 * more, so the rehearsal never substitutes a token the bot would leave
 * literal. {target} is the dashboard-facing alias of {touser}. */
export const COMMAND_SAMPLES: Record<string, string> = {
  user: 'sesame_sam',
  sender: 'sesame_sam',
  args: 'aaaa',
  touser: 'ferret_king',
  target: 'ferret_king',
  channel: 'bagel_bakery'
};

/** Rehearse a custom command response: expand, split into messages, then
 * route each line's leading slash-verb — the same order as emitResponse.
 * (Expansion per line equals whole-template expansion: no token value can
 * carry a newline, so line boundaries never move.) */
export function rehearseCommand(response: string, overrides?: Record<string, string>): RehearsedLine[] {
  const samples = { ...COMMAND_SAMPLES, ...(overrides ?? {}) };
  return responseLines(response)
    .slice(0, RESPONSE_MAX_LINES)
    .map((line) => rehearseCommandLine(line, samples));
}

/** Rehearse a module reply: one plain chat message, the module's own tokens
 * plus (unless dynamic=false — govee and clip use a bare string replacer)
 * the shared dynamic tokens. No slash-verb routing, ever. */
export function rehearseReply(
  response: string,
  samples: Record<string, string> = {},
  opts: { dynamic?: boolean } = {}
): RehearsedLine[] {
  const dynamic = opts.dynamic ?? true;
  const text = responseLines(response).join(' ');
  if (text === '') return [];
  const resolve = (key: string) =>
    key in samples ? samples[key] : dynamic ? resolveDynamic(key) : null;
  return [{ mode: 'chat', segments: expandSegments(text, resolve) }];
}

function rehearseCommandLine(line: string, samples: Record<string, string>): RehearsedLine {
  const segments = expandSegments(line, (key) => resolveCommandToken(key, samples));
  const expanded = segments.map((s) => s.text).join('');
  const action = parseSlash(expanded);
  return {
    mode: action.mode,
    verb: action.verb,
    color: action.color,
    target: action.target,
    segments: sliceSegments(segments, action.bodyStart)
  };
}

// --- token expansion (module/vars.go Expand) ------------------------------

type Resolve = (key: string) => string | null;

/** Single-pass {key} scan mirroring Go's Expand: case-SENSITIVE keys, any
 * text up to the next '}' is the key, a '{' with no closing brace is copied
 * literally through to the end. A resolved key becomes a highlighted sample;
 * an unresolved one stays literal (braces and all), marked unknown. */
export function expandSegments(text: string, resolve: Resolve): Seg[] {
  const out: Seg[] = [];
  let plainFrom = 0;
  let i = 0;
  while (i < text.length) {
    if (text[i] !== '{') {
      i++;
      continue;
    }
    const end = text.indexOf('}', i + 1);
    if (end < 0) break; // no closing brace: the rest is literal
    pushPlain(out, text.slice(plainFrom, i));
    out.push(tokenSeg(text.slice(i, end + 1), resolve(text.slice(i + 1, end))));
    plainFrom = i = end + 1;
  }
  pushPlain(out, text.slice(plainFrom));
  return out.filter((s) => s.text !== '');
}

function pushPlain(out: Seg[], text: string): void {
  if (text !== '') out.push({ text, kind: 'plain' });
}

function tokenSeg(span: string, value: string | null): Seg {
  if (value === null) return { text: span, kind: 'unknown' };
  return { text: value, kind: 'sample' };
}

/** expandCommand's lookup order: named tokens, then {counter:…}, then the
 * dynamic set. null = the bot leaves the token literal. */
function resolveCommandToken(key: string, samples: Record<string, string>): string | null {
  if (key in samples) return samples[key];
  if (key.startsWith('counter:')) return resolveCounter(key.slice('counter:'.length));
  return resolveDynamic(key);
}

/** {counter:<name>} bumps and renders the counter — a deterministic '42'
 * here. Bot-scope counters (bot:…) are admin-only and an empty name never
 * resolves, so both stay literal, exactly like the engine. */
function resolveCounter(rawName: string): string | null {
  const name = normalizeCounterName(rawName);
  if (name === '' || name.startsWith('bot:')) return null;
  return '42';
}

/** NormalizeCounterName mirror: trim, drop one leading '!', trim, lowercase. */
function normalizeCounterName(name: string): string {
  return name.trim().replace(/^!/, '').trim().toLowerCase();
}

/** ParseDynamic mirror with deterministic stand-ins so the rehearsal does not
 * re-roll on every keystroke: {random} → 57, {random:min-max} → the range
 * midpoint (invalid ranges stay literal), {choice:a,b,c} → the first option. */
function resolveDynamic(key: string): string | null {
  if (key === 'random') return '57';
  if (key.startsWith('random:')) return randomMidpoint(key.slice('random:'.length));
  if (key.startsWith('choice:')) return key.slice('choice:'.length).split(',')[0];
  return null;
}

function randomMidpoint(range: string): string | null {
  const m = range.match(/^(\d+)-(\d+)$/);
  if (!m) return null;
  const min = Number(m[1]);
  const max = Number(m[2]);
  if (max < min) return null;
  return String(Math.floor((min + max) / 2));
}

// --- slash-verb routing (engine/slash.go Translate) -----------------------

interface SlashAction {
  mode: RehearsalMode;
  verb?: string;
  color?: string;
  target?: string;
  /** Index into the expanded text where the message body starts (the verb —
   * and, for shoutout, the target — is consumed by the action). */
  bodyStart: number;
}

// Longest verbs first so "/announceblue" is not mistaken for "/announce".
const ANNOUNCE_VERBS: ReadonlyArray<readonly [string, string]> = [
  ['/announceblue', 'blue'],
  ['/announcegreen', 'green'],
  ['/announceorange', 'orange'],
  ['/announcepurple', 'purple'],
  ['/announce', 'primary']
];

/** Translate mirror over the EXPANDED text — the engine expands first, so a
 * verb produced by a token (e.g. {choice:/pin a,/pin b}) still routes. */
function parseSlash(text: string): SlashAction {
  for (const [verb, color] of ANNOUNCE_VERBS) {
    const at = verbEnd(text, verb);
    if (at !== null) return { mode: 'announce', verb, color, bodyStart: at };
  }
  const shoutoutAt = verbEnd(text, '/shoutout');
  if (shoutoutAt !== null) return parseShoutout(text, shoutoutAt);
  const pinAt = verbEnd(text, '/pin');
  if (pinAt !== null) return { mode: 'pin', verb: '/pin', bodyStart: pinAt };
  // /me is a wire passthrough (the verb stays in the text), but Twitch renders
  // it as an italic action — display it that way, verb stripped.
  const meAt = verbEnd(text, '/me');
  if (meAt !== null) return { mode: 'me', verb: '/me', bodyStart: meAt };
  return { mode: 'chat', bodyStart: 0 };
}

/** cutVerb mirror: the verb matches as the whole string or as a "verb "
 * prefix; returns the index just past the verb and its one separating space,
 * or null when the verb does not lead the text. */
function verbEnd(text: string, verb: string): number | null {
  if (text === verb) return verb.length;
  if (text.startsWith(verb + ' ')) return verb.length + 1;
  return null;
}

/** /shoutout <target> — the first token (leading '@' dropped) becomes the
 * target; the body is what follows, left-trimmed, like the engine's Cut. */
function parseShoutout(text: string, from: number): SlashAction {
  let i = from;
  while (text[i] === ' ') i++;
  let j = i;
  while (j < text.length && text[j] !== ' ') j++;
  const target = text.slice(i, j).replace(/^@/, '');
  while (text[j] === ' ') j++;
  return { mode: 'shoutout', verb: '/shoutout', target, bodyStart: j };
}

/** Drop the first `from` characters from a segment list, preserving the
 * sample/unknown marks of whatever remains. */
function sliceSegments(segments: Seg[], from: number): Seg[] {
  if (from <= 0) return segments;
  const out: Seg[] = [];
  let skip = from;
  for (const seg of segments) {
    if (skip >= seg.text.length) {
      skip -= seg.text.length;
      continue;
    }
    out.push(skip > 0 ? { ...seg, text: seg.text.slice(skip) } : seg);
    skip = 0;
  }
  return out;
}
