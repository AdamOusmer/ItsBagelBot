// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Chat-rehearsal core: the dashboard-side mirror of how the bot expands and
// routes a response template. Every rule here corresponds to one place in the
// Go engine, keep them in lockstep:
//
//   - token lexing:          pkg/tmpl/tmpl.go (Lex, Token.Resolve), ported to ./tmpl
//   - scope chain:           app/twitch/sesame/engine/scope (Chain, Pure, Message, Viewer, Store)
//   - chain wiring per run:  app/twitch/sesame/engine/vars.go (commandChain)
//   - counter normalization: app/twitch/sesame/engine/scope/store.go (NormalizeName)
//   - slash-verb routing:    internal/domain/outgress/slash.go (CutSlash)
//   - emit order + line cap: app/twitch/sesame/engine/dispatch.go (emitResponse)
//
// The engine resolves a template through an ordered chain of scopes: the first
// scope that owns a token name answers it, a scope whose dependency is missing
// is simply absent from the chain, and a name no mounted scope owns stays
// literal. This file mirrors that shape with sample-value scopes, so "which
// tokens light up in the preview" is decided by the same mechanism that
// decides it in chat, rather than by a parallel if-chain that can drift.
//
// The marketing site's command builder imports this module too (aliased
// @bagel/rehearsal), so it stays pure: plain data in, plain data out, no DOM
// and no framework. Each surface renders the RehearsedLine[] its own way.
//
// The bot has exactly two expansion behaviors, so there are exactly two
// rehearsals. Slash-verbs route on EVERY path: the pipeline's emit (and
// outgress's sendBotLine for the clip reply) translates a leading /announce,
// /shoutout, /pin after expansion, so both rehearsals render the native
// action; they differ only in which scopes are mounted and in fan-out:
//
//   rehearseCommand: custom "!command" responses. The engine expands the
//   whole template first, then splits it into lines (cap 5, one chat message
//   each), then translates each line's leading slash-verb.
//
//   rehearseReply: module replies (alerts, trigger words, channel-point
//   rewards, built-ins, gossip commands). Each module expands ONLY its own
//   token map: most fall back to the shared dynamic tokens ({random},
//   {choice:…}), a few (govee, clip) do not, and emits one message, whose
//   leading slash-verb routes the same way.

import { RESPONSE_MAX_LINES, responseLines } from './commands-validate';
import { queryEscape, resolveComputedUtil, UTIL_NAMES } from './pure';
import { lex, resolveToken, type VarToken } from './tmpl';

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

/** Sample values, keyed the way VarToken.key is built: "name", or
 * "name:payload". */
export type Samples = Readonly<Record<string, string>>;

/** One "{…}" span, as ./tmpl lexes it: a lower-cased name, a case-preserved
 * payload (null when the span carries no ':' at all), an optional fallback,
 * the raw span and the "name"/"name:payload" lookup key. */
export type Token = VarToken;

/** Resolve one token to its rehearsed value; null leaves it literal. */
export type Resolve = (token: Token) => string | null;

/** One family of tokens and the sample values that stand in for it — the
 * preview's mirror of a Go scope.Scope. A scope that does not own a name
 * declines it, and the chain moves on; a name nobody owns stays literal,
 * which is exactly what the bot does with a token no mounted scope answers. */
export interface SampleScope {
  owns(name: string): boolean;
  get(token: Token): string | null;
}

/** Sample values for the canonical tokens the message scope resolves, nothing
 * more, so the rehearsal never substitutes a token the bot would leave
 * literal. {sender} and {target} are absent on purpose: they are aliases
 * (see COMMAND_ALIASES), so overriding the canonical token covers both. */
export const COMMAND_SAMPLES: Samples = {
  user: 'sesame_sam',
  args: 'ferret_king good luck',
  touser: 'ferret_king',
  channel: 'bagel_bakery',
  userid: '48291057',
  'user.login': 'sesame_sam',
  command: 'hug'
};

/** The message scope resolves each pair to one value ({user}/{sender} are both
 * the chatter, {touser}/{target} both the mentioned name), so the alias
 * canonicalizes before lookup and a single override covers its partner. */
const COMMAND_ALIASES: Samples = { sender: 'user', target: 'touser' };

/** The fixed names the message scope owns, matching scope.Message's
 * messageFields table. The positional names ({1}…{30}) are not listed: they
 * are recognized by shape, like the Go scope's positionalIndex. */
const MESSAGE_NAMES = new Set([
  'user',
  'sender',
  'args',
  'touser',
  'target',
  'channel',
  'userid',
  'user.login',
  'command',
  'querystring'
]);

/** The highest word a template may address, matching scope.MaxPositional.
 * Past it a span stays literal, so "since {2024}" is visible rather than
 * silently empty. */
const MAX_POSITIONAL = 30;

/** Deterministic stand-ins for values the bot rolls or reads at run time, so
 * the rehearsal shows something the bot could produce without re-rolling on
 * every keystroke. */
const RANDOM_SAMPLE = '57';
const COUNTER_SAMPLE = '42';

/** {countdown}/{countup} read the wall clock, so the preview shows a fixed,
 * plausible span instead of a live one: a value that ticks while the
 * broadcaster types would redraw the rehearsal on a timer and still not be
 * the value chat sees, since chat sees it whenever the command runs. The
 * wording matches the bot's shared humanizer (the same one !uptime prints
 * through, internal/domain/i18n HumanizeDuration). */
const COUNTDOWN_SAMPLE = '3 days, 4 hours';

/** Stand-ins for the viewer lookups (scope.Viewer): a follow date, an account
 * creation date and a loyalty standing all live in services the dashboard
 * cannot reach while the broadcaster is typing, so the preview shows a
 * plausible answer rather than a live one. The two spans are worded by the
 * bot's shared humanizer, like every other span it prints. */
const FOLLOWAGE_SAMPLE = '3 months';
const ACCOUNTAGE_SAMPLE = '4 years, 2 months';
const POINTS_SAMPLE = '1280';
const POINTS_NAME_SAMPLE = 'bagels';
const WATCHTIME_SAMPLE = '2 hours, 30 minutes';

/** Rehearse a custom command response: expand, split into messages, then
 * route each line's leading slash-verb (the same order as emitResponse).
 * (Expansion per line equals whole-template expansion: no token value can
 * carry a newline, so line boundaries never move.) */
export function rehearseCommand(response: string, overrides?: Samples): RehearsedLine[] {
  const resolve = chainResolver(commandChain({ ...COMMAND_SAMPLES, ...(overrides ?? {}) }));
  return responseLines(response)
    .slice(0, RESPONSE_MAX_LINES)
    .map((line) => rehearseLine(line, resolve));
}

/** Rehearse a module reply: one message, the module's own tokens plus
 * (unless dynamic=false, govee and clip use a bare string replacer) the
 * shared dynamic tokens. A leading slash-verb routes exactly like a command
 * line: the pipeline translates every emitted output. */
export function rehearseReply(
  response: string,
  samples: Samples = {},
  opts: ReplyOptions = {}
): RehearsedLine[] {
  const text = responseLines(response).join(' ');
  if (text === '') return [];
  return [rehearseLine(text, chainResolver(replyChain(samples, opts)))];
}

/** How a module reply rehearses. `dynamic` is false for the two modules that
 * replace tokens with a bare string replacer (govee, clip) rather than going
 * through ParseDynamic, so the shared dynamic tokens must NOT be shown
 * resolving for them. */
export interface ReplyOptions {
  dynamic?: boolean;
}

/** One chat message: expand tokens, then route the leading slash-verb over
 * the EXPANDED text (the engine's order), so a verb minted by a token (e.g.
 * {choice:/pin a,/pin b}) still routes. */
function rehearseLine(line: string, resolve: Resolve): RehearsedLine {
  const segments = expandSegments(line, resolve);
  const action = parseSlash(segments.map((seg) => seg.text).join(''));
  return {
    mode: action.mode,
    verb: action.verb,
    color: action.color,
    target: action.target,
    segments: sliceSegments(segments, action)
  };
}

// --- token expansion (pkg/tmpl Lex + scope.Chain.Render) ------------------

/** Lex the text and turn each token into a segment: a resolved token becomes
 * a highlighted sample (its fallback text when it resolves to empty, exactly
 * like Token.Resolve), an unresolved one stays literal — braces, payload and
 * fallback included — marked unknown so a typo is visible.
 *
 * Exported because a surface may want to rehearse against its own resolver
 * rather than one of the two chains below. */
export function expandSegments(text: string, resolve: Resolve): Seg[] {
  const out: Seg[] = [];
  for (const token of lex(text)) {
    if (token.kind === 'literal') {
      out.push({ text: token.text, kind: 'plain' });
      continue;
    }
    out.push(segFor(token, resolve));
  }
  return out.filter((seg) => seg.text !== '');
}

function segFor(token: Token, resolve: Resolve): Seg {
  const value = resolve(token);
  return { text: resolveToken(token, value), kind: value === null ? 'unknown' : 'sample' };
}

// --- scopes (engine/scope) ------------------------------------------------

/** Walk a chain the way scope.Chain does: the first scope that owns the name
 * answers it, and a name nobody owns is left literal. */
function chainResolver(chain: readonly SampleScope[]): Resolve {
  return (token) => {
    const scope = chain.find((s) => s.owns(token.name));
    return scope ? scope.get(token) : null;
  };
}

/** commandChain's mirror: the dice and the payload utilities (one Go scope,
 * scope.Pure), the triggering line, the viewer lookups, then the counter
 * store. Order is precedence, exactly as in the engine. */
function commandChain(samples: Samples): SampleScope[] {
  return [PURE_SCOPE, UTIL_SCOPE, messageScope(samples), VIEWER_SCOPE, COUNTER_SCOPE];
}

/** A module reply resolves only its own token map, plus the dynamic set when
 * that module falls back to ParseDynamic. There is no message, counter or
 * utility scope: a module reply is not a custom command, and the Go side
 * expands it through module.ParseDynamic rather than through scope.Pure — so
 * {math:…} and friends stay literal there, exactly as they do in chat. */
function replyChain(samples: Samples, opts: ReplyOptions): SampleScope[] {
  const own: SampleScope = {
    owns: () => true,
    get: (token) => (token.key in samples ? samples[token.key] : null)
  };
  return (opts.dynamic ?? true) ? [PURE_SCOPE, own] : [own];
}

/** scope.Pure's mirror: {random} → a fixed stand-in, {random:min-max} → the
 * range midpoint (an invalid range stays literal), {choice:a,b,c} → the first
 * option. A bare {choice} names no options and stays literal, like
 * ParseDynamic returning ok=false. */
const PURE_SCOPE: SampleScope = {
  owns: (name) => name === 'random' || name === 'choice',
  get: (token) => (token.name === 'choice' ? choiceSample(token) : randomSample(token))
};

function choiceSample(token: Token): string | null {
  return token.payload === null ? null : token.payload.split(',')[0];
}

function randomSample(token: Token): string | null {
  if (token.payload === null) return RANDOM_SAMPLE;
  const bounds = token.payload.match(/^(\d+)-(\d+)$/);
  if (!bounds) return null;
  const min = Number(bounds[1]);
  const max = Number(bounds[2]);
  return max < min ? null : String(Math.floor((min + max) / 2));
}

/** The rest of scope.Pure: the utilities that take a payload. The computed
 * ones ({math:…}, {queryescape:…}, {pathescape:…}, {repeat:n:phrase}) are
 * evaluated for real by ./pure, so the preview prints the bytes chat will
 * print; the clock ones show a fixed sample. A utility with no payload names
 * nothing to work on ({math} is not the token) and stays literal.
 *
 * It is a second scope here while Go has one, because the Go split falls the
 * other way: scope.Pure is mounted only on custom commands, while the dice
 * below are ALSO the module-reply palette (module.ParseDynamic), so PURE_SCOPE
 * has to be mountable on its own. */
const UTIL_SCOPE: SampleScope = {
  owns: (name) => UTIL_NAMES.has(name),
  get: utilSample
};

function utilSample(token: Token): string | null {
  if (token.payload === null) return null;
  if (token.name === 'countdown' || token.name === 'countup') return countdownSample(token.payload);
  return resolveComputedUtil(token);
}

/** A parseable date previews as a fixed span; anything else resolves to the
 * empty string (not null), so the span's fallback renders exactly as it would
 * in chat when the bot fails to read the date. */
function countdownSample(payload: string): string {
  return isInstant(payload) ? COUNTDOWN_SAMPLE : '';
}

/** The two spellings scope.parseInstant accepts: RFC3339, or a bare
 * YYYY-MM-DD read as UTC midnight. The shape is checked before Date.parse so
 * the preview does not accept the many other formats JavaScript will
 * ("Dec 25 2026", "2026/12/25") and the bot will not. */
const RFC3339 = /^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/;
const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;

function isInstant(payload: string): boolean {
  const text = payload.trim();
  if (!RFC3339.test(text) && !DATE_ONLY.test(text)) return false;
  return !Number.isNaN(Date.parse(text));
}

/** scope.Message's mirror: the identity and argument tokens the chat line
 * already carries. A payload is declined rather than ignored ({user:bob} is
 * not a token), matching Message.Get — except on a positional name, where
 * the empty payload is the rest-of-args form. */
function messageScope(samples: Samples): SampleScope {
  return {
    owns: (name) => MESSAGE_NAMES.has(name) || positionalIndex(name) !== null,
    get: (token) => messageSample(token, samples)
  };
}

function messageSample(token: Token, samples: Samples): string | null {
  if (positionalIndex(token.name) !== null) return positionalSample(token, samples);
  if (token.payload !== null) return null;
  // {querystring} is derived from the args sample rather than carried as one,
  // for the same reason the positional words are: a surface that overrides
  // {args} must not get a URL-encoded preview that disagrees with the {args}
  // beside it. It runs through the same encoder as {queryescape:…}.
  if (token.name === 'querystring') return queryEscape(samples.args ?? '');
  const name = COMMAND_ALIASES[token.name] ?? token.name;
  return name in samples ? samples[name] : null;
}

/** A positional word number in 1..MAX_POSITIONAL, or null for a span that
 * only looks like one: a sign, a leading zero, {0} and anything past the cap
 * are not this token and stay literal (scope.positionalIndex). */
function positionalIndex(name: string): number | null {
  if (!/^[1-9][0-9]?$/.test(name)) return null;
  const n = Number(name);
  return n <= MAX_POSITIONAL ? n : null;
}

/** {n} is word n of the {args} sample, {n:} is words n to the end.
 *
 * Derived from the args sample rather than carrying samples of its own, the
 * way the engine derives them from the same argument string: a surface that
 * overrides {args} gets positional samples that agree with it, instead of a
 * preview where {1} contradicts {args}. A word past the end resolves to the
 * empty string (not null), so the span's fallback renders exactly as it would
 * in chat. Any other payload is the {n:m} slice this grammar does not have,
 * so it stays literal. */
function positionalSample(token: Token, samples: Samples): string | null {
  const n = positionalIndex(token.name);
  if (n === null) return null;
  if (token.payload !== null && token.payload !== '') return null;
  const words = (samples.args ?? '').split(/\s+/).filter((word) => word !== '');
  if (n > words.length) return '';
  return token.payload === null ? words[n - 1] : words.slice(n - 1).join(' ');
}

/** scope.Viewer's mirror: the tokens that describe ONE viewer through a
 * lookup — {followage}, {accountage}, {points}, {watchtime} — plus the
 * channel's {pointsname}. Bare is the caller, a payload names somebody else,
 * and the preview shows the same stand-in either way: the number chat sees
 * depends on who runs the command, which is exactly what a preview cannot
 * know.
 *
 * It is mounted unconditionally here, as COUNTER_SCOPE already is, and for
 * the same reason: the surfaces that rehearse a template pass a response and
 * nothing else, so there is no module state to gate on. In chat the engine
 * mounts each family only while its module is on (Followage, Account age,
 * Loyalty Points), and an off module leaves its spans literal — so a
 * broadcaster with one of them off sees a token here that stays visible
 * there. The guide and the chip hints say so; a rehearsal that took module
 * state would have to be threaded through every caller to say it in one more
 * place.
 *
 * A span that addresses nobody ({points:}) or a currency name handed a viewer
 * ({pointsname:bob}) stays literal, matching refOf in the Go scope. */
const VIEWER_SAMPLES: Samples = {
  followage: FOLLOWAGE_SAMPLE,
  accountage: ACCOUNTAGE_SAMPLE,
  points: POINTS_SAMPLE,
  watchtime: WATCHTIME_SAMPLE
};

const VIEWER_SCOPE: SampleScope = {
  owns: (name) => name in VIEWER_SAMPLES || name === 'pointsname',
  get: viewerSample
};

function viewerSample(token: Token): string | null {
  if (token.name === 'pointsname') return token.payload === null ? POINTS_NAME_SAMPLE : null;
  // A payload is the named-viewer form; an empty one names nobody, exactly as
  // an empty login does in the engine.
  if (token.payload !== null && token.payload.trim().replace(/^@/, '') === '') return null;
  return VIEWER_SAMPLES[token.name];
}

/** scope.Store's mirror: {counter:<name>} bumps and renders the counter. The
 * name normalizes like NormalizeName (trim, drop one leading '!', trim,
 * lower-case). A {counter:target:<name>} spelling keys the bump on the
 * mentioned viewer instead of the sender (issue #479); it rehearses the same
 * way once the addressing prefix comes off. Bot-scope counters (bot:…) are
 * admin-only and an empty name never resolves, so both stay literal, exactly
 * like the engine. */
const COUNTER_SCOPE: SampleScope = {
  owns: (name) => name === 'counter',
  get: counterSample
};

function counterSample(token: Token): string | null {
  // A bare {counter} is not the counter form: with no payload it names no
  // counter and falls through literal, exactly as HasPayload does in the
  // engine.
  const name = (token.payload ?? '').trim().replace(/^!/, '').trim().toLowerCase();
  const base = name.startsWith('target:') ? name.slice('target:'.length) : name;
  if (base === '' || base.startsWith('bot:')) return null;
  return COUNTER_SAMPLE;
}

// --- slash-verb routing (outgress/slash.go CutSlash) ----------------------

interface SlashAction {
  mode: RehearsalMode;
  verb?: string;
  color?: string;
  target?: string;
  /** Index into the expanded text where the message body starts (the verb,
   * and for shoutout the target, is consumed by the action). */
  bodyStart: number;
}

interface VerbSpec {
  verb: string;
  mode: RehearsalMode;
  /** Announce accent color; absent for the verbs that carry none. */
  color?: string;
}

// Longest verbs first so "/announceblue" is not mistaken for "/announce".
const VERBS: readonly VerbSpec[] = [
  { verb: '/announceblue', mode: 'announce', color: 'blue' },
  { verb: '/announcegreen', mode: 'announce', color: 'green' },
  { verb: '/announceorange', mode: 'announce', color: 'orange' },
  { verb: '/announcepurple', mode: 'announce', color: 'purple' },
  { verb: '/announce', mode: 'announce', color: 'primary' },
  { verb: '/shoutout', mode: 'shoutout' },
  { verb: '/pin', mode: 'pin' },
  { verb: '/me', mode: 'me' }
];

/** CutSlash mirror over the EXPANDED text (the engine expands first, so a
 * verb produced by a token still routes). /me is a wire passthrough (the verb
 * stays in the text), but Twitch renders it as an italic action, so it is
 * displayed that way with the verb stripped. */
function parseSlash(text: string): SlashAction {
  for (const spec of VERBS) {
    const at = verbEnd(text, spec);
    if (at === null) continue;
    if (spec.mode === 'shoutout') {
      const shoutout = parseShoutout(text.slice(at));
      return { ...shoutout, bodyStart: at + shoutout.bodyStart };
    }
    return { mode: spec.mode, verb: spec.verb, color: spec.color, bodyStart: at };
  }
  return { mode: 'chat', bodyStart: 0 };
}

/** cutVerb mirror: the verb matches as the whole string or as a "verb "
 * prefix; returns the index just past the verb and its one separating space,
 * or null when the verb does not lead the text. */
function verbEnd(text: string, spec: VerbSpec): number | null {
  if (text === spec.verb) return spec.verb.length;
  if (text.startsWith(spec.verb + ' ')) return spec.verb.length + 1;
  return null;
}

/** /shoutout <target>: the first token (leading '@' dropped) becomes the
 * target; the body is what follows, left-trimmed, like the engine's Cut.
 *
 * `text` is what FOLLOWS the verb, and bodyStart is an offset into that, so
 * parseSlash adds the verb back on: this half of the rule does not need to
 * know where in the line it was found. */
function parseShoutout(text: string): SlashAction {
  let i = 0;
  while (text[i] === ' ') i++;
  let j = i;
  while (j < text.length && text[j] !== ' ') j++;
  const target = text.slice(i, j).replace(/^@/, '');
  while (text[j] === ' ') j++;
  return { mode: 'shoutout', verb: '/shoutout', target, bodyStart: j };
}

/** Drop the routed verb from a segment list: everything before the action's
 * bodyStart goes, and the sample/unknown marks of whatever remains stay. */
function sliceSegments(segments: Seg[], action: SlashAction): Seg[] {
  if (action.bodyStart <= 0) return segments;
  const out: Seg[] = [];
  let skip = action.bodyStart;
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
