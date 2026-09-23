// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Chat-rehearsal core: the dashboard-side mirror of how the bot expands and
// routes a response template. Every rule here corresponds to one place in the
// Go engine, keep them in lockstep:
//
//   - token lexing:          pkg/tmpl/tmpl.go (Lex, Token.Resolve), ported to ./tmpl
//   - scope chain:           app/twitch/sesame/engine/scope (Chain, Pure, Message, Chatters, Emotes, Channel, Viewer, Modules, Store)
//   - chain wiring per run:  app/twitch/sesame/engine/vars.go (commandChain)
//   - counter normalization: app/twitch/sesame/engine/scope/store.go (NormalizeName)
//   - slash-verb routing:    internal/domain/outgress/slash.go (CutSlash)
//   - emit order + line cap: app/twitch/sesame/engine/dispatch.go (emitCommand)
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
import { condText, type Cond, lex, parseCond, resolveToken, type VarToken } from './tmpl';
import {
  ACCOUNTAGE_SAMPLE,
  ARGS_SAMPLE,
  BTTV_EMOTES_SAMPLE,
  CHANNEL_SAMPLE,
  CHANNEL_VIEWERS_SAMPLE,
  CHATTERS_SAMPLE,
  COMMAND_SAMPLE,
  COUNTDOWN_SAMPLE,
  COUNTER_SAMPLE,
  FFZ_EMOTES_SAMPLE,
  FOLLOWAGE_SAMPLE,
  FOLLOWERS_SAMPLE,
  GAME_SAMPLE,
  POINTS_NAME_SAMPLE,
  POINTS_SAMPLE,
  QUOTE_SAMPLE,
  RANDOM_CHATTER_SAMPLE,
  RANDOM_EMOTE_SAMPLE,
  RANDOM_SAMPLE,
  RANDOM_VIEWER_SAMPLE,
  SEVENTV_EMOTES_SAMPLE,
  SONG_ARTIST_SAMPLE,
  SONG_TITLE_SAMPLE,
  SUBS_SAMPLE,
  TIME_PLACE_SAMPLE,
  TIME_SAMPLE,
  TITLE_SAMPLE,
  TOUSER_SAMPLE,
  UPTIME_SAMPLE,
  URLFETCH_SAMPLE,
  USER_LOGIN_SAMPLE,
  USER_SAMPLE,
  USERID_SAMPLE,
  USES_SAMPLE,
  WATCHTIME_SAMPLE
} from '../variables/preview-values';

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

/** The two fields owns() ever needs to decide ownership: a token's name and
 * (for the two scopes that split one name between two families — uses' bare
 * {count} vs. the store's {count:<name>}, scope.Uses/scope.Store's
 * Owns(v Var) mirror) its payload. A real VarToken satisfies this
 * structurally, so a caller holding one passes it straight through; a caller
 * that only has a bare name (ownedByCore, timerOwns, resolvedWithoutSample —
 * none of which have a lexed token to hand) builds the object with payload
 * left undefined. One object param, not two loose strings, is what keeps
 * this file's scope helpers off CodeScene's Primitive Obsession radar. */
export interface TokenQuery {
  name: string;
  payload?: string | null;
}

/** One family of tokens and the sample values that stand in for it — the
 * preview's mirror of a Go scope.Scope. A scope that does not own a name
 * declines it, and the chain moves on; a name nobody owns stays literal,
 * which is exactly what the bot does with a token no mounted scope answers. */
export interface SampleScope {
  owns(query: TokenQuery): boolean;
  get(token: Token): string | null;
}

/** Sample values for the canonical tokens the message scope resolves, nothing
 * more, so the rehearsal never substitutes a token the bot would leave
 * literal. {sender} and {target} are absent on purpose: they are aliases
 * (see COMMAND_ALIASES), so overriding the canonical token covers both. */
export const COMMAND_SAMPLES: Samples = {
  user: USER_SAMPLE,
  args: ARGS_SAMPLE,
  touser: TOUSER_SAMPLE,
  channel: CHANNEL_SAMPLE,
  'user.id': USERID_SAMPLE,
  'user.login': USER_LOGIN_SAMPLE,
  command: COMMAND_SAMPLE
};

/** The message scope resolves each pair to one value ({user}/{sender} are both
 * the chatter, {touser}/{target} both the mentioned name), so the alias
 * canonicalizes before lookup and a single override covers its partner.
 * {userid} is the pre-simplification spelling of {user.id} (scope.
 * messageAliases), kept resolving the same way. */
const COMMAND_ALIASES: Samples = { sender: 'user', target: 'touser', userid: 'user.id' };

/** The fixed names the message scope owns, matching scope.Message's
 * messageFields table plus its one legacy alias. The positional names
 * ({1}…{30}) are not listed: they are recognized by shape, like the Go
 * scope's positionalIndex — and neither is the empty name ({:m}), recognized
 * the same way (see messageOwns). */
const MESSAGE_NAMES = new Set([
  'user',
  'sender',
  'args',
  'touser',
  'target',
  'channel',
  'user.id',
  'userid',
  'user.login',
  'command',
  'querystring'
]);

/** The highest word a template may address, matching scope.MaxPositional.
 * Past it a span stays literal, so "since {2024}" is visible rather than
 * silently empty. */
const MAX_POSITIONAL = 30;

// The *_SAMPLE consts these scopes used to declare locally now live in
// ../variables/preview-values.ts, imported above: ./variables (the guide, the
// parity test) needs the exact same values, and a second copy is how a
// rehearsal preview and a guide page drift (docs/specs/variables-catalog.md
// D2, D8).

/** Rehearse a custom command response: expand, split into messages, then
 * route each line's leading slash-verb (the same order as emitCommand).
 * (Expansion per line equals whole-template expansion: no token value can
 * carry a newline, so line boundaries never move.) */
export function rehearseCommand(response: string, overrides?: Samples): RehearsedLine[] {
  const resolve = chainResolver(commandChain({ ...COMMAND_SAMPLES, ...(overrides ?? {}) }));
  return responseLines(response)
    .map((line) => expandSegments(line, resolve))
    .filter((segments) => !isBlankLine(segments))
    .slice(0, RESPONSE_MAX_LINES)
    .map(routeLine);
}

/** Whether an expanded line has nothing visible left on it, which a false
 * {if:…} with no else can do to a line that was written with words on it.
 * The engine drops such a line BEFORE counting it against the five-message
 * cap (engine/dispatch.go blankLine), so this filter runs before the slice
 * for the same reason: a reply must not lose its last line because an earlier
 * one went quiet. */
function isBlankLine(segments: Seg[]): boolean {
  return segments.every((seg) => seg.text.trim() === '');
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

/** Rehearse a timer message: one message, Go's timerChain mirror (timerOwns's
 * own comment says which scopes mount and why: no message, counter or viewer
 * scope, since a tick has no chatter behind it). No sample overrides — unlike
 * a module reply, a timer's tokens are never caller-supplied, they are
 * exactly what the chain itself answers. */
export function rehearseTimer(response: string): RehearsedLine[] {
  const text = responseLines(response).join(' ');
  return text === '' ? [] : [rehearseLine(text, chainResolver(timerChain()))];
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
  return routeLine(expandSegments(line, resolve));
}

/** The slash-verb half of rehearseLine, over segments that are already
 * expanded: the command rehearsal drops the lines a conditional emptied
 * before routing what is left, so it expands first and routes after. */
function routeLine(segments: Seg[]): RehearsedLine {
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

function segFor(token: VarToken, resolve: Resolve): Seg {
  const cond = parseCond(token);
  if (cond !== null) return condSeg(token, cond, resolve);
  const value = resolve(token);
  return { text: resolveToken(token, value), kind: value === null ? 'unknown' : 'sample' };
}

/** A conditional's segment: the branch its cond chose, from the sample value
 * of the token the cond NAMES (scope.Chain.Render's renderSpan). Nothing in
 * reach owning that name leaves the whole span literal and marked unknown,
 * exactly as chat would print it, so a cond naming a module that is off is
 * visible here rather than silently taking the else branch. */
function condSeg(token: VarToken, cond: Cond, resolve: Resolve): Seg {
  const value = resolve(cond.ref);
  return { text: condText(token, cond, value), kind: value === null ? 'unknown' : 'sample' };
}

// --- scopes (engine/scope) ------------------------------------------------

/** Walk a chain the way scope.Chain does: the first scope that owns the name
 * answers it, and a name nobody owns is left literal. */
function chainResolver(chain: readonly SampleScope[]): Resolve {
  return (token) => {
    const scope = chain.find((s) => s.owns(token));
    return scope ? scope.get(token) : null;
  };
}

/** commandChain's mirror: the dice and the payload utilities (one Go scope,
 * scope.Pure), the triggering line, the chat room, the emote catalog, the
 * channel, the viewer lookups, the module facts, then the counter store. Order
 * is precedence, exactly as in the engine. */
function commandChain(samples: Samples): SampleScope[] {
  return [
    PURE_SCOPE,
    UTIL_SCOPE,
    messageScope(samples),
    USES_SCOPE,
    CHATTER_SCOPE,
    EMOTE_SCOPE,
    CHANNEL_SCOPE,
    VIEWER_SCOPE,
    MODULE_SCOPE,
    COUNTER_SCOPE,
    EXTERNAL_SCOPE
  ];
}

/**
 * Whether any scope on the custom-command chain claims `name` (lower-cased,
 * as ./tmpl folds it).
 *
 * This is the one published answer to "does the core already resolve this
 * token?", so a palette chip, a builder catalog entry or a guide example can
 * be ASSERTED against the chain instead of being matched by a hand-written
 * regex that has to be kept in step with it. The marketing site's command
 * builder carried such a regex and it had already drifted: it missed
 * {pointsname}, which VIEWER_SCOPE has owned since the loyalty tokens landed,
 * so the builder shipped a sample value for a token the core resolves.
 *
 * COMMAND_SAMPLES is passed because messageScope takes its sample map, but no
 * scope's `owns` reads it: ownership is a fact about the name, values are not.
 *
 * The conditional is owned without being a scope: {if:…} is resolved by the
 * renderer (segFor -> parseCond), one level above the chain, because it reads
 * the value of the token its cond NAMES rather than one of its own. It belongs
 * in this answer all the same — a surface must not hand the core a sample
 * value for "if".
 */
export function ownedByCore(name: string): boolean {
  if (name === COND_TOKEN_NAME) return true;
  return commandChain(COMMAND_SAMPLES).some((scope) => scope.owns({ name }));
}

/** The span name ./tmpl's parseCond claims. */
const COND_TOKEN_NAME = 'if';

/** Go's timer surface (app/twitch/sesame/engine/timer_vars.go, timerChain):
 * the scopes a timer's message can resolve, minus everything commandChain
 * mounts from the triggering chat line (messageScope, USES_SCOPE) or from
 * who typed it (VIEWER_SCOPE, COUNTER_SCOPE) — a tick has no chatter behind
 * it to supply either. What is left is every scope already safe with nobody
 * watching: the dice, the chat room, the emote catalog, the channel facts,
 * the module facts, and (like every other surface) EXTERNAL_SCOPE's fixed
 * stand-in for {urlfetch:…}. web/kit/lib/variables/surfaces.ts's
 * forSurface('timer') is the one caller; the golden fixture (parity.test.ts)
 * is what keeps this list and Go's TimerFamilies() from drifting apart. */
export function timerOwns(name: string): boolean {
  return timerChain().some((scope) => scope.owns({ name }));
}

/** The scopes a timer tick mounts (see timerOwns's own comment): a function
 * rather than a top-level array so it can sit here, beside the ownership
 * check it backs, while still referencing scopes declared further down this
 * file (CHATTER_SCOPE, CHANNEL_SCOPE, MODULE_SCOPE) — a top-level const at
 * this point would read them before their own declarations run. rehearseTimer
 * (below) calls this too, so the preview and the ownership check can never
 * mount a different set. */
function timerChain(): SampleScope[] {
  return [PURE_SCOPE, UTIL_SCOPE, CHATTER_SCOPE, EMOTE_SCOPE, CHANNEL_SCOPE, MODULE_SCOPE, EXTERNAL_SCOPE];
}

/** Which chain a surface rehearses against: a custom command's full scope
 * chain, or a module reply's (the dice plus that reply's own token map). */
export type ChainKind = 'command' | 'reply';

/**
 * Whether the core answers `name` from a stand-in of ITS OWN on that chain —
 * i.e. whether a sample value a surface hands in for it would simply never be
 * read.
 *
 * This is the question a surface building a sample map actually has, and it is
 * NOT the same as ownedByCore. Two scopes read the caller's values rather than
 * carrying their own:
 *
 *   - the message scope, on the command chain: rehearseCommand's overrides are
 *     exactly its values, so a surface that wants its own {user} gets it.
 *   - the reply chain's own token map, which is the caller's map entire; only
 *     the dice (scope.Pure, mounted ahead of it) answer for themselves there.
 *
 * The marketing builder asked this with a hand-written 40-branch regex over
 * token text, which is why {pointsname} shipped a sample the viewer scope then
 * ignored, and why {time} and {points} were suppressed on module-reply
 * surfaces that DO resolve them — the regex could not tell the two chains
 * apart, and a name that means one thing on one chain means another on the
 * other.
 */
export function resolvedWithoutSample(name: string, kind: ChainKind): boolean {
  if (kind === 'reply') return PURE_SCOPE.owns({ name });
  return !messageOwns(name) && ownedByCore(name);
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
  owns: ({ name }) => name === 'random' || name === 'choice',
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
  owns: ({ name }) => UTIL_NAMES.has(name),
  get: utilSample
};

function utilSample(token: Token): string | null {
  if (token.payload === null) return null;
  if (token.name === 'countdown' || token.name === 'countup') return countdownSample({ text: token.payload });
  return resolveComputedUtil(token);
}

/** A parseable date previews as a fixed span; anything else resolves to the
 * empty string (not null), so the span's fallback renders exactly as it would
 * in chat when the bot fails to read the date. */
function countdownSample(input: { text: string }): string {
  return isInstant(input) ? COUNTDOWN_SAMPLE : '';
}

/** The two spellings scope.parseInstant accepts: RFC3339, or a bare
 * YYYY-MM-DD read as UTC midnight. The shape is checked before Date.parse so
 * the preview does not accept the many other formats JavaScript will
 * ("Dec 25 2026", "2026/12/25") and the bot will not. */
const RFC3339 = /^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/;
const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;

function isInstant(input: { text: string }): boolean {
  const text = input.text.trim();
  if (!RFC3339.test(text) && !DATE_ONLY.test(text)) return false;
  return !Number.isNaN(Date.parse(text));
}

/** scope.Message's mirror: the identity and argument tokens the chat line
 * already carries. A payload is declined rather than ignored ({user:bob} is
 * not a token), matching Message.Get — except on a positional name, where
 * the empty payload is the rest-of-args form. */
function messageScope(samples: Samples): SampleScope {
  return {
    owns: ({ name }) => messageOwns(name),
    get: (token) => messageSample(token, samples)
  };
}

/** The message scope's ownership, as a plain predicate: it is asked outside
 * the chain too (see resolvedWithoutSample), because this is the ONE scope
 * whose values a caller supplies. The empty name is the {:m} leading-slice
 * form (scope.Message.Owns("") === true); a bad payload on it ({}, {:x},
 * {:}) is still OWNED here and messageSample is what turns it literal, the
 * same split every payload-taking token in this grammar uses. */
function messageOwns(name: string): boolean {
  return MESSAGE_NAMES.has(name) || positionalIndex({ text: name }) !== null || name === '';
}

function messageSample(token: Token, samples: Samples): string | null {
  if (token.name === '') return leadingSliceSample(token, samples);
  if (positionalIndex({ text: token.name }) !== null) return positionalSample(token, samples);
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
 * are not this token and stay literal (scope.positionalIndex).
 *
 * Takes `{ text }` rather than a bare string: this and the other small
 * string/number helpers below it were the file's actual Primitive Obsession
 * weight (CodeScene scores this per FILE, so the fix that matters is
 * cutting bare-primitive PARAMS across all of them, not just the
 * SampleScope.owns() family the TokenQuery type above already covers). Every
 * caller already holds either a token field or another wrapped value, so
 * this costs a one-word object literal at each call site, not a new type a
 * caller has to learn. */
function positionalIndex(input: { text: string }): number | null {
  if (!/^[1-9][0-9]?$/.test(input.text)) return null;
  const n = Number(input.text);
  return n <= MAX_POSITIONAL ? n : null;
}

/** The {args} sample split into words, the way the engine splits Words from
 * the argument line. Every positional/slice sample reads from this so a
 * surface overriding {args} gets positional and slice samples that agree
 * with it. */
function argWords(samples: Samples): string[] {
  return (samples.args ?? '').split(/\s+/).filter((word) => word !== '');
}

/** {n} is word n; {n:} is words n..end; {n:m} is words n..m inclusive — the
 * same grammar and clamps as scope.Message.positional/slice: m past the end
 * clamps to the end, m before n or non-numeric stays literal, and n itself
 * past the end resolves to "" (not null) so the span's fallback renders
 * exactly as it would in chat. */
function positionalSample(token: Token, samples: Samples): string | null {
  const n = positionalIndex({ text: token.name });
  if (n === null) return null;
  const words = argWords(samples);
  if (token.payload === null) return n > words.length ? '' : words[n - 1];
  if (token.payload === '') return wordSlice(words, { n, end: words.length });
  const m = positionalIndex({ text: token.payload });
  if (m === null || m < n) return null;
  return wordSlice(words, { n, end: m });
}

/** {:m}: the empty name with a numeric payload is words 1..m, same rules as
 * {n:m}. Every other empty-name span ({}, {:}, {:x}) stays literal. */
function leadingSliceSample(token: Token, samples: Samples): string | null {
  if (token.payload === null) return null;
  const m = positionalIndex({ text: token.payload });
  if (m === null) return null;
  return wordSlice(argWords(samples), { n: 1, end: m });
}

/** Words n..end inclusive, space-joined; end clamps to the word count, and n
 * past the word count renders "" rather than null (a resolved-empty span, so
 * its fallback fires — matching scope.Message.slice). */
function wordSlice(words: string[], { n, end }: { n: number; end: number }): string {
  if (n > words.length) return '';
  return words.slice(n - 1, Math.min(end, words.length)).join(' ');
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
  owns: ({ name }) => name in VIEWER_SAMPLES || name === 'points.name' || name === 'pointsname',
  get: viewerSample
};

function viewerSample(token: Token): string | null {
  // {points.name} is the canonical spelling; {pointsname} is the pre-
  // simplification alias scope.PointsNameLegacyToken keeps resolving.
  if (token.name === 'points.name' || token.name === 'pointsname') {
    return token.payload === null ? POINTS_NAME_SAMPLE : null;
  }
  // A payload is the named-viewer form; an empty one names nobody, exactly as
  // an empty login does in the engine.
  if (token.payload !== null && token.payload.trim().replace(/^@/, '') === '') return null;
  return VIEWER_SAMPLES[token.name];
}

/** scope.Uses' mirror: {uses} and bare {count} (the canonical spelling,
 * scope/uses.go), how many times this custom command has been run in this
 * channel.
 *
 * The preview shows a stand-in because the real number is the command row's
 * own counter, which the surfaces that rehearse a template do not carry. Two
 * things about it are visible here anyway, and both are what a broadcaster
 * gets wrong first: the number never includes the run being previewed, and it
 * is approximate — the bot batches use ticks, so chat can be up to about a
 * minute behind the true total.
 *
 * Mounted unconditionally here, as the counter store is: no module gates it.
 * In chat it is mounted only on a custom command, so {uses} typed into a
 * module's own reply stays literal there — replyChain leaves it literal here
 * for the same reason, since this scope is not in that chain.
 *
 * owns reads payload to split the name "count" from COUNTER_SCOPE, which
 * answers {count:<name>} (a payload) rather than this bare form — the exact
 * split scope.Uses.Owns/scope.Store.Owns make on the Go side by inspecting
 * the whole Var. Without it, chain.find's name-only lookup would always stop
 * at whichever of the two scopes comes first, which is what motivated
 * SampleScope.owns taking payload at all. */
const USES_SCOPE: SampleScope = {
  owns: ({ name, payload }) => name === 'uses' || (name === 'count' && (payload ?? null) === null),
  get: (token) => (token.payload === null ? USES_SAMPLE : null)
};

/** scope.Chatters' mirror: {chatters}, the size of the room; {random.chatter},
 * one name from who has spoken; and {random.viewer}, one name from who
 * Twitch reports as connected right now — a different source (see
 * RANDOM_VIEWER_SAMPLE), mounted the same unconditional way.
 *
 * This is the one scope in the chain that is mounted unconditionally in CHAT
 * as well as here: no module gates it, because the bot reads the chatters it
 * has watched speak (or, for {random.viewer}, the shared chat-list cache)
 * rather than asking Twitch fresh. So unlike the viewer lookups and the
 * module facts, a token previewed here can never be one that stays literal
 * in chat — an unknown room is the count "0" and an empty draw, both real
 * answers. What DOES differ is the wording: chat draws {random.chatter} from
 * recently active chatters, not from everyone with the page open, which the
 * guide says and a preview cannot show.
 *
 * None of the three tokens takes a payload, so a span carrying one stays
 * literal, matching the Go scope. */
const CHATTER_SAMPLES: Samples = {
  chatters: CHATTERS_SAMPLE,
  'random.chatter': RANDOM_CHATTER_SAMPLE,
  // {random.viewer}: who Twitch reports as connected right now, a different
  // source from {random.chatter} beside it (see RANDOM_VIEWER_SAMPLE), but
  // mounted the same way — no module gates either.
  'random.viewer': RANDOM_VIEWER_SAMPLE
};

const CHATTER_SCOPE: SampleScope = {
  owns: ({ name }) => name in CHATTER_SAMPLES,
  get: (token) => (token.payload === null ? CHATTER_SAMPLES[token.name] : null)
};

/** scope.Emotes' mirror: {emotes:<provider>} and {random.emote}.
 *
 * Mounted here always, and in chat whenever the service loads the catalog at
 * all — no module gates these, because the codes are refreshed for the
 * automod's false-positive suppression rather than for a feature a
 * broadcaster switches on. So a token previewed here stays literal in chat
 * only on a deployment that loads no codes.
 *
 * Two things the preview cannot show and the guide says instead: these are
 * the GLOBAL sets every channel has, not the emotes a broadcaster added to
 * their own channel, and the real lists are truncated to fit one chat line.
 * A catalog that has not loaded yet renders empty rather than literal, so a
 * fallback speaks.
 *
 * {7tvemotes}/{bttvemotes}/{ffzemotes} are the pre-simplification bare
 * spellings (scope's SevenTVEmotesToken etc.), kept resolving the same lists.
 * None of the four legacy names or {random.emote} takes a payload, so a span
 * carrying one stays literal, matching the Go scope; {emotes:<provider>} is
 * the one name here where a payload is the point. */
const EMOTE_SAMPLES: Samples = {
  '7tvemotes': SEVENTV_EMOTES_SAMPLE,
  bttvemotes: BTTV_EMOTES_SAMPLE,
  ffzemotes: FFZ_EMOTES_SAMPLE,
  'random.emote': RANDOM_EMOTE_SAMPLE
};

/** {emotes:<provider>} payload dispatch, matching scope.emoteProviders. */
const EMOTE_PROVIDER_SAMPLES: Samples = {
  '7tv': SEVENTV_EMOTES_SAMPLE,
  bttv: BTTV_EMOTES_SAMPLE,
  ffz: FFZ_EMOTES_SAMPLE
};

const EMOTE_SCOPE: SampleScope = {
  owns: ({ name }) => name in EMOTE_SAMPLES || name === 'emotes',
  get: emoteSample
};

function emoteSample(token: Token): string | null {
  if (token.name === 'emotes') {
    return token.payload === null ? null : (EMOTE_PROVIDER_SAMPLES[token.payload.toLowerCase()] ?? null);
  }
  return token.payload === null ? EMOTE_SAMPLES[token.name] : null;
}

/** scope.Channel's mirror: {uptime}, {title}, {game}, {channel.viewers} and
 * {followers}/{subs}, the facts about the channel rather than the person who
 * ran the command. Bare is this channel, a payload names somebody else's for
 * the first three, and the preview shows the same stand-in either way.
 *
 * Mounted unconditionally here, as the viewer lookups and module facts are,
 * and with the same caveat: in chat {uptime}, {title} and {game} are each
 * gated by the same per-broadcaster toggle as the command that prints them
 * (!uptime, !title, !game), so a broadcaster who switched one off sees a
 * token here that stays literal there. The chip hints say which.
 * {channel.viewers} and {followers}/{subs} have no toggle at all — no
 * command prints any of the three, so mounting follows the dependency alone.
 *
 * {channel.viewers} takes no payload — it counts this channel — so a span
 * carrying one stays literal, and so does an empty payload on the other
 * three, matching loginOf in the Go scope. Bare {channel} stays the display
 * name, and is answered by the message scope well before this one.
 *
 * It stays dotted rather than the bare {viewers} a simplification pass tried:
 * {viewers} already names the raid/shoutout reply's party size
 * (modules/reply_tokens.go), and one word cannot mean two different numbers
 * (see scope.ViewersToken's own comment). */
const CHANNEL_SAMPLES: Samples = {
  uptime: UPTIME_SAMPLE,
  title: TITLE_SAMPLE,
  game: GAME_SAMPLE,
  'channel.viewers': CHANNEL_VIEWERS_SAMPLE,
  // {followers}/{subs}: like channel.viewers, no toggle gates either one and
  // neither takes a payload (see NO_PAYLOAD_CHANNEL_NAMES) — they always
  // count THIS channel, never a named one.
  followers: FOLLOWERS_SAMPLE,
  subs: SUBS_SAMPLE
};

/** Names on this scope that count only the calling channel and take no
 * payload at all, the way {channel.viewers} always did — {uptime}/{title}/
 * {game} are the three that accept one to name another channel. */
const NO_PAYLOAD_CHANNEL_NAMES = new Set(['channel.viewers', 'followers', 'subs']);

const CHANNEL_SCOPE: SampleScope = {
  owns: ({ name }) => name in CHANNEL_SAMPLES,
  get: channelSample
};

function channelSample(token: Token): string | null {
  if (token.payload === null) return CHANNEL_SAMPLES[token.name];
  if (NO_PAYLOAD_CHANNEL_NAMES.has(token.name)) return null;
  // A payload is the named-channel form; an empty one names nobody, exactly
  // as an empty login does in the engine.
  return token.payload.trim().replace(/^@/, '') === '' ? null : CHANNEL_SAMPLES[token.name];
}

/** scope.Modules' mirror: the tokens whose value is a single fact an opt-in
 * module already holds — {quote} / {quote:n}, {time} / {time:<place>}, and
 * the {song} family.
 *
 * Mounted unconditionally here, as COUNTER_SCOPE and VIEWER_SCOPE already
 * are, and for the same reason: the surfaces that rehearse a template pass a
 * response and nothing else, so there is no module state to gate on. In chat
 * the engine mounts each family only while its module is on (Quotes, Local
 * Time, Song Requests), and an off module leaves its spans literal — so a
 * broadcaster with one of them off sees a token here that stays visible
 * there. The guide and the chip hints say so. {time:<place>} is the one
 * exception: it answers ungated in chat too, so it previews the same
 * regardless of the Local Time module's state.
 *
 * {quote:n} previews the same stand-in as the bare draw: the preview cannot
 * know quote #7's text, and inventing a second fake quote for it would
 * suggest it could. A payload that is not a positive number, and any payload
 * at all on a {song…} span, stays literal, matching the Go scope. {time} is
 * the one name here where a payload means something (see timeSample): it is
 * NOT gated by the module the way the bare form is, so it previews the same
 * on every channel. */
const MODULE_SAMPLES: Samples = {
  time: TIME_SAMPLE,
  song: `${SONG_TITLE_SAMPLE} by ${SONG_ARTIST_SAMPLE}`,
  'song.title': SONG_TITLE_SAMPLE,
  'song.artist': SONG_ARTIST_SAMPLE
};

const MODULE_SCOPE: SampleScope = {
  owns: ({ name }) => name === 'quote' || name in MODULE_SAMPLES,
  get: moduleSample
};

function moduleSample(token: Token): string | null {
  if (token.name === 'quote') return quoteSample(token);
  if (token.name === 'time') return timeSample(token);
  return token.payload === null ? MODULE_SAMPLES[token.name] : null;
}

function quoteSample(token: Token): string | null {
  if (token.payload === null) return QUOTE_SAMPLE;
  return /^\s*[1-9][0-9]*\s*$/.test(token.payload) ? QUOTE_SAMPLE : null;
}

/** {time}/{time:<place>}: unlike every other name on this scope, the payload
 * form is NOT gated the same way the bare one is (see scope.Places' decision
 * record) — it needs no Local Time enrollment, so it previews even where the
 * bare form's chip hint says the module is off. An empty payload addresses
 * nobody and stays literal, matching Normalize("") in the Go scope; the
 * preview cannot know whether a given place resolves, so any other payload
 * shows the same plausible stand-in, exactly as quoteSample does for a quote
 * number. */
function timeSample(token: Token): string | null {
  if (token.payload === null) return MODULE_SAMPLES.time;
  return token.payload.trim() === '' ? null : TIME_PLACE_SAMPLE;
}

/** scope.Store's mirror: {counter:<name>} and {count:<name>} both READ the
 * counter — the write moved to the command's own "bump a counter" option
 * (see app/db/commands/ent/schema/commands.go's bump_counter field; a
 * template no longer bumps anything). Both spell a counter the same way, so
 * one parser answers both, and the preview shows one number for either. The
 * name normalizes like NormalizeName (trim, drop one leading '!', trim,
 * lower-case). A {counter:target:<name>} spelling reads the mentioned
 * viewer's own bucket instead of the channel's (issue #479); it rehearses
 * the same way once the addressing prefix comes off. An empty name never
 * resolves, so it stays literal, exactly like the engine.
 *
 * owns claims bare "counter" unconditionally (it takes no payload-less
 * form, so a bare {counter} simply renders literal via get below, the same
 * outcome as not owning it at all) but "count" only WITH a payload — bare
 * {count} belongs to USES_SCOPE. See that scope's owns for why payload
 * matters to the split. */
const COUNTER_SCOPE: SampleScope = {
  owns: ({ name, payload }) => name === 'counter' || (name === 'count' && (payload ?? null) !== null),
  get: counterSample
};

function normalizeCounterName(payload: string | null): string {
  const name = (payload ?? '').trim().replace(/^!/, '').trim().toLowerCase();
  return name.startsWith('target:') ? name.slice('target:'.length) : name;
}

function counterSample(token: Token): string | null {
  const base = normalizeCounterName(token.payload);
  if (base === '') return null;
  return COUNTER_SAMPLE;
}

/** scope.External's mirror: {urlfetch:<definition>}. A preview cannot make a
 * live HTTP call, so this is a fixed plausible stand-in (URLFETCH_SAMPLE)
 * rather than a live answer — but it is a REAL, resolvable token on both
 * chains that mount it, and marking it "unknown" (the same red mark a typo
 * gets) told a broadcaster their spelling was wrong when it was not. Mounted
 * on commandChain and timerChain alike, so a custom command's and a timer's
 * previews read the same way for the one family neither can actually fetch.
 * An empty payload names no source and stays literal, matching the Go
 * scope's Normalize(""). */
const EXTERNAL_SCOPE: SampleScope = {
  owns: ({ name }) => name === 'urlfetch',
  get: (token) => (token.payload === null || token.payload.trim() === '' ? null : URLFETCH_SAMPLE)
};

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
