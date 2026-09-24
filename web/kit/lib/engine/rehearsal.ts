// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export interface Seg {
  text: string;
  kind: SegKind;
}

export type RehearsalMode = 'chat' | 'announce' | 'shoutout' | 'pin' | 'me';

export interface RehearsedLine {
  mode: RehearsalMode;
  verb?: string;
  color?: string;
  target?: string;
  segments: Seg[];
}

export type Samples = Readonly<Record<string, string>>;

export type Token = VarToken;

export type Resolve = (token: Token) => string | null;

export interface TokenQuery {
  name: string;
  payload?: string | null;
}

export interface SampleScope {
  owns(query: TokenQuery): boolean;
  get(token: Token): string | null;
}

export const COMMAND_SAMPLES: Samples = {
  user: USER_SAMPLE,
  args: ARGS_SAMPLE,
  touser: TOUSER_SAMPLE,
  channel: CHANNEL_SAMPLE,
  'user.id': USERID_SAMPLE,
  'user.login': USER_LOGIN_SAMPLE,
  command: COMMAND_SAMPLE
};

const COMMAND_ALIASES: Samples = { sender: 'user', target: 'touser', userid: 'user.id' };

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

const MAX_POSITIONAL = 30;

export function rehearseCommand(response: string, overrides?: Samples): RehearsedLine[] {
  const resolve = chainResolver(commandChain({ ...COMMAND_SAMPLES, ...(overrides ?? {}) }));
  return responseLines(response)
    .map((line) => expandSegments(line, resolve))
    .filter((segments) => !isBlankLine(segments))
    .slice(0, RESPONSE_MAX_LINES)
    .map(routeLine);
}

function isBlankLine(segments: Seg[]): boolean {
  return segments.every((seg) => seg.text.trim() === '');
}

export function rehearseReply(
  response: string,
  samples: Samples = {},
  opts: ReplyOptions = {}
): RehearsedLine[] {
  const text = responseLines(response).join(' ');
  if (text === '') return [];
  return [rehearseLine(text, chainResolver(replyChain(samples, opts)))];
}

export function rehearseTimer(response: string): RehearsedLine[] {
  const text = responseLines(response).join(' ');
  return text === '' ? [] : [rehearseLine(text, chainResolver(timerChain()))];
}

export interface ReplyOptions {
  dynamic?: boolean;
}

function rehearseLine(line: string, resolve: Resolve): RehearsedLine {
  return routeLine(expandSegments(line, resolve));
}

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

function condSeg(token: VarToken, cond: Cond, resolve: Resolve): Seg {
  const value = resolve(cond.ref);
  return { text: condText(token, cond, value), kind: value === null ? 'unknown' : 'sample' };
}

function chainResolver(chain: readonly SampleScope[]): Resolve {
  return (token) => {
    const scope = chain.find((s) => s.owns(token));
    return scope ? scope.get(token) : null;
  };
}

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

export function ownedByCore(name: string): boolean {
  if (name === COND_TOKEN_NAME) return true;
  return commandChain(COMMAND_SAMPLES).some((scope) => scope.owns({ name }));
}

const COND_TOKEN_NAME = 'if';

export function timerOwns(name: string): boolean {
  return timerChain().some((scope) => scope.owns({ name }));
}

function timerChain(): SampleScope[] {
  return [PURE_SCOPE, UTIL_SCOPE, CHATTER_SCOPE, EMOTE_SCOPE, CHANNEL_SCOPE, MODULE_SCOPE, EXTERNAL_SCOPE];
}

export type ChainKind = 'command' | 'reply';

export function resolvedWithoutSample(name: string, kind: ChainKind): boolean {
  if (kind === 'reply') return PURE_SCOPE.owns({ name });
  return !messageOwns(name) && ownedByCore(name);
}

function replyChain(samples: Samples, opts: ReplyOptions): SampleScope[] {
  const own: SampleScope = {
    owns: () => true,
    get: (token) => (token.key in samples ? samples[token.key] : null)
  };
  return (opts.dynamic ?? true) ? [PURE_SCOPE, own] : [own];
}

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

const UTIL_SCOPE: SampleScope = {
  owns: ({ name }) => UTIL_NAMES.has(name),
  get: utilSample
};

function utilSample(token: Token): string | null {
  if (token.payload === null) return null;
  if (token.name === 'countdown' || token.name === 'countup') return countdownSample({ text: token.payload });
  return resolveComputedUtil(token);
}

function countdownSample(input: { text: string }): string {
  return isInstant(input) ? COUNTDOWN_SAMPLE : '';
}

const RFC3339 = /^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$/;
const DATE_ONLY = /^\d{4}-\d{2}-\d{2}$/;

function isInstant(input: { text: string }): boolean {
  const text = input.text.trim();
  if (!RFC3339.test(text) && !DATE_ONLY.test(text)) return false;
  return !Number.isNaN(Date.parse(text));
}

function messageScope(samples: Samples): SampleScope {
  return {
    owns: ({ name }) => messageOwns(name),
    get: (token) => messageSample(token, samples)
  };
}

function messageOwns(name: string): boolean {
  return MESSAGE_NAMES.has(name) || positionalIndex({ text: name }) !== null || name === '';
}

function messageSample(token: Token, samples: Samples): string | null {
  if (token.name === '') return leadingSliceSample(token, samples);
  if (positionalIndex({ text: token.name }) !== null) return positionalSample(token, samples);
  if (token.payload !== null) return null;
  if (token.name === 'querystring') return queryEscape(samples.args ?? '');
  const name = COMMAND_ALIASES[token.name] ?? token.name;
  return name in samples ? samples[name] : null;
}

function positionalIndex(input: { text: string }): number | null {
  if (!/^[1-9][0-9]?$/.test(input.text)) return null;
  const n = Number(input.text);
  return n <= MAX_POSITIONAL ? n : null;
}

function argWords(samples: Samples): string[] {
  return (samples.args ?? '').split(/\s+/).filter((word) => word !== '');
}

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

function leadingSliceSample(token: Token, samples: Samples): string | null {
  if (token.payload === null) return null;
  const m = positionalIndex({ text: token.payload });
  if (m === null) return null;
  return wordSlice(argWords(samples), { n: 1, end: m });
}

function wordSlice(words: string[], { n, end }: { n: number; end: number }): string {
  if (n > words.length) return '';
  return words.slice(n - 1, Math.min(end, words.length)).join(' ');
}

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
  if (token.name === 'points.name' || token.name === 'pointsname') {
    return token.payload === null ? POINTS_NAME_SAMPLE : null;
  }
  if (token.payload !== null && token.payload.trim().replace(/^@/, '') === '') return null;
  return VIEWER_SAMPLES[token.name];
}

const USES_SCOPE: SampleScope = {
  owns: ({ name, payload }) => name === 'uses' || (name === 'count' && (payload ?? null) === null),
  get: (token) => (token.payload === null ? USES_SAMPLE : null)
};

const CHATTER_SAMPLES: Samples = {
  chatters: CHATTERS_SAMPLE,
  'random.chatter': RANDOM_CHATTER_SAMPLE,
  'random.viewer': RANDOM_VIEWER_SAMPLE
};

const CHATTER_SCOPE: SampleScope = {
  owns: ({ name }) => name in CHATTER_SAMPLES,
  get: (token) => (token.payload === null ? CHATTER_SAMPLES[token.name] : null)
};

const EMOTE_SAMPLES: Samples = {
  '7tvemotes': SEVENTV_EMOTES_SAMPLE,
  bttvemotes: BTTV_EMOTES_SAMPLE,
  ffzemotes: FFZ_EMOTES_SAMPLE,
  'random.emote': RANDOM_EMOTE_SAMPLE
};

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

const CHANNEL_SAMPLES: Samples = {
  uptime: UPTIME_SAMPLE,
  title: TITLE_SAMPLE,
  game: GAME_SAMPLE,
  'channel.viewers': CHANNEL_VIEWERS_SAMPLE,
  followers: FOLLOWERS_SAMPLE,
  subs: SUBS_SAMPLE
};

const NO_PAYLOAD_CHANNEL_NAMES = new Set(['channel.viewers', 'followers', 'subs']);

const CHANNEL_SCOPE: SampleScope = {
  owns: ({ name }) => name in CHANNEL_SAMPLES,
  get: channelSample
};

function channelSample(token: Token): string | null {
  if (token.payload === null) return CHANNEL_SAMPLES[token.name];
  if (NO_PAYLOAD_CHANNEL_NAMES.has(token.name)) return null;
  return token.payload.trim().replace(/^@/, '') === '' ? null : CHANNEL_SAMPLES[token.name];
}

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

function timeSample(token: Token): string | null {
  if (token.payload === null) return MODULE_SAMPLES.time;
  return token.payload.trim() === '' ? null : TIME_PLACE_SAMPLE;
}

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

const EXTERNAL_SCOPE: SampleScope = {
  owns: ({ name }) => name === 'urlfetch',
  get: (token) => (token.payload === null || token.payload.trim() === '' ? null : URLFETCH_SAMPLE)
};

interface SlashAction {
  mode: RehearsalMode;
  verb?: string;
  color?: string;
  target?: string;
  bodyStart: number;
}

interface VerbSpec {
  verb: string;
  mode: RehearsalMode;
  color?: string;
}

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

function verbEnd(text: string, spec: VerbSpec): number | null {
  if (text === spec.verb) return spec.verb.length;
  if (text.startsWith(spec.verb + ' ')) return spec.verb.length + 1;
  return null;
}

function parseShoutout(text: string): SlashAction {
  let i = 0;
  while (text[i] === ' ') i++;
  let j = i;
  while (j < text.length && text[j] !== ' ') j++;
  const target = text.slice(i, j).replace(/^@/, '');
  while (text[j] === ' ') j++;
  return { mode: 'shoutout', verb: '/shoutout', target, bodyStart: j };
}

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
