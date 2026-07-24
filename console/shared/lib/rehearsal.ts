// Chat-rehearsal core: the dashboard-side mirror of how the bot expands and
// routes a response template. Every rule here corresponds to one place in the
// Go engine — keep them in lockstep:
//
//   - token expansion:       app/sesame/module/vars.go (Expand, ParseDynamic)
//   - command tokens:        app/sesame/engine/vars.go (expandCommand)
//   - counter normalization: app/sesame/engine/loyalty_valkey.go (NormalizeCounterName)
//   - slash-verb routing:    internal/domain/outgress/slash.go (CutSlash)
//   - emit order + line cap: app/sesame/engine/dispatch.go (emitResponse)
//
// The marketing site's command builder imports this module too (aliased
// @bagel/rehearsal), so it stays pure: plain data in, plain data out, no DOM
// and no framework. Each surface renders the RehearsedLine[] its own way.
//
// The bot has exactly two expansion behaviors, so there are exactly two
// rehearsals. Slash-verbs route on EVERY path — the pipeline's emit (and
// outgress's sendBotLine for the clip reply) translates a leading /announce,
// /shoutout, /pin after expansion — so both rehearsals render the native
// action; they differ only in tokens and fan-out:
//
//   rehearseCommand — custom "!command" responses. The engine expands the
//   whole template first, then splits it into lines (cap 5, one chat message
//   each), then translates each line's leading slash-verb.
//
//   rehearseReply — module replies (alerts, trigger words, channel-point
//   rewards, built-ins, gateway commands). Each module expands ONLY its own
//   token map — most fall back to the shared dynamic tokens ({random},
//   {choice:…}), a few (govee, clip) do not — and emits one message, whose
//   leading slash-verb routes the same way.

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
  /** The matched slash verb, e.g. "/announcegreen". */
  verb?: string;
  /** Announce accent color key ("primary" for the bare verb). */
  color?: string;
  /** Shoutout target with the leading '@' removed. */
  target?: string;
  segments: Seg[];
}

/** Sample values, keyed the way Token.key is built. */
export type Samples = Readonly<Record<string, string>>;

/**
 * One "{…}" span, split the way module.Expand reads it. The name is
 * lower-cased (token names are case-insensitive) while the payload after the
 * first ':' keeps its case, so {CHOICE:Hi,Yo} still offers "Hi".
 *
 * payload is null when the span carries no ':' at all — the engine draws a
 * real distinction there: {choice} is unknown and stays literal, while
 * {choice:} is an (empty) option list that resolves.
 */
export interface Token {
  /** The span exactly as written, braces included. */
  span: string;
  name: string;
  payload: string | null;
  /** "name", or "name:payload" — what a sample map is keyed by. */
  key: string;
}

/** Resolve one token to its rehearsed value; null leaves it literal. */
export type Resolve = (token: Token) => string | null;

/** Sample values for the canonical tokens expandCommand resolves — nothing
 * more, so the rehearsal never substitutes a token the bot would leave
 * literal. {sender} and {target} are absent on purpose: they are aliases
 * (see COMMAND_ALIASES), so overriding the canonical token covers both. */
export const COMMAND_SAMPLES: Samples = {
  user: 'sesame_sam',
  args: 'aaaa',
  touser: 'ferret_king',
  channel: 'bagel_bakery'
};

/** expandCommand resolves each pair to one value ({user}/{sender} are both the
 * chatter, {touser}/{target} both the mentioned name), so the alias
 * canonicalizes before lookup and a single override covers its partner. */
const COMMAND_ALIASES: Samples = { sender: 'user', target: 'touser' };

/** Deterministic stand-ins for values the bot rolls at run time, so the
 * rehearsal shows something the bot could produce without re-rolling on
 * every keystroke. */
const RANDOM_SAMPLE = '57';
const COUNTER_SAMPLE = '42';

/** Rehearse a custom command response: expand, split into messages, then
 * route each line's leading slash-verb — the same order as emitResponse.
 * (Expansion per line equals whole-template expansion: no token value can
 * carry a newline, so line boundaries never move.) */
export function rehearseCommand(response: string, overrides?: Samples): RehearsedLine[] {
  const samples: Samples = { ...COMMAND_SAMPLES, ...(overrides ?? {}) };
  return responseLines(response)
    .slice(0, RESPONSE_MAX_LINES)
    .map((line) => rehearseLine(line, (token) => resolveCommandToken(token, samples)));
}

/** Rehearse a module reply: one message, the module's own tokens plus
 * (unless dynamic=false — govee and clip use a bare string replacer) the
 * shared dynamic tokens. A leading slash-verb routes exactly like a command
 * line: the pipeline translates every emitted output. */
export function rehearseReply(
  response: string,
  samples: Samples = {},
  opts: { dynamic?: boolean } = {}
): RehearsedLine[] {
  const dynamic = opts.dynamic ?? true;
  const text = responseLines(response).join(' ');
  if (text === '') return [];
  return [rehearseLine(text, (token) => resolveReplyToken(token, samples, dynamic))];
}

/** One chat message: expand tokens, then route the leading slash-verb over
 * the EXPANDED text — the engine's order, so a verb minted by a token (e.g.
 * {choice:/pin a,/pin b}) still routes. */
function rehearseLine(line: string, resolve: Resolve): RehearsedLine {
  const segments = expandSegments(line, resolve);
  const action = parseSlash(segments.map((seg) => seg.text).join(''));
  return {
    mode: action.mode,
    verb: action.verb,
    color: action.color,
    target: action.target,
    segments: sliceSegments(segments, action.bodyStart)
  };
}

// --- token expansion (module/vars.go Expand) ------------------------------

/** Single-pass {key} scan mirroring Go's Expand: any text up to the next '}'
 * is the token, and a '{' with no closing brace is copied literally through
 * to the end. A resolved token becomes a highlighted sample; an unresolved
 * one stays literal (braces and all), marked unknown. */
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
    out.push(segFor(parseToken(text.slice(i, end + 1)), resolve));
    plainFrom = i = end + 1;
  }
  pushPlain(out, text.slice(plainFrom));
  return out.filter((seg) => seg.text !== '');
}

function pushPlain(out: Seg[], text: string): void {
  if (text !== '') out.push({ text, kind: 'plain' });
}

function segFor(token: Token, resolve: Resolve): Seg {
  const value = resolve(token);
  if (value === null) return { text: token.span, kind: 'unknown' };
  return { text: value, kind: 'sample' };
}

/** Split a "{…}" span into its case-folded name and case-preserved payload. */
function parseToken(span: string): Token {
  const body = span.slice(1, -1);
  const colon = body.indexOf(':');
  if (colon < 0) {
    const name = body.toLowerCase();
    return { span, name, payload: null, key: name };
  }
  const name = body.slice(0, colon).toLowerCase();
  const payload = body.slice(colon + 1);
  return { span, name, payload, key: `${name}:${payload}` };
}

// --- resolvers ------------------------------------------------------------

/** expandCommand's lookup order: named tokens (aliases folded in), then
 * {counter:…}, then the dynamic set. */
function resolveCommandToken(token: Token, samples: Samples): string | null {
  const sample = lookupSample(token, samples, COMMAND_ALIASES);
  if (sample !== null) return sample;
  // A bare {counter} is not the counter form; it falls through like any
  // other unknown name, exactly as CutPrefix does in the engine.
  if (token.name === 'counter' && token.payload !== null) return resolveCounter(token);
  return resolveDynamic(token);
}

/** A module reply resolves only its own token map, plus the dynamic set when
 * that module falls back to ParseDynamic. */
function resolveReplyToken(token: Token, samples: Samples, dynamic: boolean): string | null {
  const sample = lookupSample(token, samples);
  if (sample !== null) return sample;
  return dynamic ? resolveDynamic(token) : null;
}

function lookupSample(token: Token, samples: Samples, aliases: Samples = {}): string | null {
  const name = aliases[token.name] ?? token.name;
  const key = token.payload === null ? name : `${name}:${token.payload}`;
  return key in samples ? samples[key] : null;
}

/** {counter:<name>} bumps and renders the counter. Bot-scope counters
 * (bot:…) are admin-only and an empty name never resolves, so both stay
 * literal, exactly like the engine. */
function resolveCounter(token: Token): string | null {
  const name = normalizeCounterName(token.payload ?? '');
  if (name === '' || name.startsWith('bot:')) return null;
  return COUNTER_SAMPLE;
}

/** NormalizeCounterName mirror: trim, drop one leading '!', trim, lowercase. */
function normalizeCounterName(name: string): string {
  return name.trim().replace(/^!/, '').trim().toLowerCase();
}

/** ParseDynamic mirror: {random} → a fixed stand-in, {random:min-max} → the
 * range midpoint (invalid ranges stay literal), {choice:a,b,c} → the first
 * option. */
function resolveDynamic(token: Token): string | null {
  switch (token.name) {
    case 'random':
      return token.payload === null ? RANDOM_SAMPLE : randomMidpoint(token.payload);
    case 'choice':
      return token.payload === null ? null : token.payload.split(',')[0];
    default:
      return null;
  }
}

function randomMidpoint(range: string): string | null {
  const bounds = range.match(/^(\d+)-(\d+)$/);
  if (!bounds) return null;
  const min = Number(bounds[1]);
  const max = Number(bounds[2]);
  if (max < min) return null;
  return String(Math.floor((min + max) / 2));
}

// --- slash-verb routing (outgress/slash.go CutSlash) ----------------------

interface SlashAction {
  mode: RehearsalMode;
  verb?: string;
  color?: string;
  target?: string;
  /** Index into the expanded text where the message body starts (the verb —
   * and, for shoutout, the target — is consumed by the action). */
  bodyStart: number;
}

interface AnnounceVerb {
  verb: string;
  color: string;
}

// Longest verbs first so "/announceblue" is not mistaken for "/announce".
const ANNOUNCE_VERBS: readonly AnnounceVerb[] = [
  { verb: '/announceblue', color: 'blue' },
  { verb: '/announcegreen', color: 'green' },
  { verb: '/announceorange', color: 'orange' },
  { verb: '/announcepurple', color: 'purple' },
  { verb: '/announce', color: 'primary' }
];

/** CutSlash mirror over the EXPANDED text — the engine expands first, so a
 * verb produced by a token still routes. /me is a wire passthrough (the verb
 * stays in the text), but Twitch renders it as an italic action, so it is
 * displayed that way with the verb stripped. */
function parseSlash(text: string): SlashAction {
  for (const entry of ANNOUNCE_VERBS) {
    const at = verbEnd(text, entry.verb);
    if (at !== null) return { mode: 'announce', verb: entry.verb, color: entry.color, bodyStart: at };
  }
  const shoutoutAt = verbEnd(text, '/shoutout');
  if (shoutoutAt !== null) return parseShoutout(text, shoutoutAt);
  const pinAt = verbEnd(text, '/pin');
  if (pinAt !== null) return { mode: 'pin', verb: '/pin', bodyStart: pinAt };
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
