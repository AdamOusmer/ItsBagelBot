// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Command validation shared by the dashboard server action and the client
// editor, so the instant client-side feedback and the authoritative server
// check can never disagree.
//
// Normalization mirrors the commands service: the stored key never carries the
// leading "!" and is lower-case; chat keeps the "!" to invoke.

export const COMMAND_NAME_MAX = 64;
/** Per line — each line is sent as its own chat message (Twitch limit). */
export const RESPONSE_MAX = 500;
/** A response is newline-delimited: the bot sends one message per line. */
export const RESPONSE_MAX_LINES = 5;
export const COOLDOWN_MAX = 86400;

// --- urlfetch definition rules ---------------------------------------------
//
// The numbers below are the shared contract between this console (instant
// client feedback AND the authoritative server re-check in the fetches page
// actions) and the commands/gossip services' Go validators. They live here —
// not inline in the UI — so client and server literally cannot drift.

/** Stored bare/lower-case like a command trigger. Matches Go FetchDefName
 * `^[a-z0-9_]{1,32}$` — one grammar for `{urlfetch:name}` payload heads. */
export const FETCH_NAME_MAX = 32;
/** https only; long enough for signed query strings (512 measured across the
 * APIs broadcasters actually wire up), rejected beyond. */
export const FETCH_URL_MAX = 512;
/** Depth of the dotted JSON path. Mirrors the Go resolver's cap; deeper paths
 * are almost always a sign the author picked the wrong leaf. */
export const JSON_PATH_MAX_DEPTH = 8;
/** Distinct `{urlfetch:…}` references allowed in one command response. Each
 * distinct name fans out to gossip before the line renders, so the cap bounds
 * the chat-latency worst case (3 × ~2.5s in parallel, never serial). */
export const URLFETCH_TOKEN_CAP = 3;
/** Names the sealed key a def uses; display-only label, not the secret. */
export const KEY_LABEL_MAX = 32;
/** Sealed key material ceiling; the value crosses the wire exactly once. */
export const KEY_VALUE_MAX = 512;
/** Definitions per broadcaster. Chat expansion resolves every token in a
 * response, so 20 defs bounds one broadcaster's share of the fan-out budget;
 * enforced synchronously at save by the commands service (COUNT before
 * insert), mirrored here only as an early, friendly stop. */
export const DEFS_PER_BROADCASTER = 20;

const FETCH_NAME_RE = /^[a-z0-9_]+$/;
// Path segments and array indices: indices `\d+` are a subset of the name
// charset, so one regex covers both (mirrors the Go resolver's two rules).
const PATH_SEGMENT_RE = /^[A-Za-z0-9_-]+$/;

export type FetchKind = 'plain' | 'json';

/** The bare trigger discipline of normName applied to a def slug: trim,
 * lower-case, fold every non-grammar rune run to "_", trim "_" edges. Empty
 * when the input carries no usable character at all. */
export function slugifyName(s: string): string {
  // Underscore edges are trimmed by index scan, not /^_+/ and /_+$/ — those
  // anchors backtrack polynomially on adversarial runs of "_" (CodeQL
  // js/polynomial-redos), and this input is broadcaster-typed. The remaining
  // char-class fold is unambiguous and linear.
  const folded = s.trim().toLowerCase().replace(/[^a-z0-9_]+/g, '_');
  return trimUnderscores(trimUnderscores(folded).slice(0, FETCH_NAME_MAX));
}

function trimUnderscores(s: string): string {
  let start = 0;
  let end = s.length;
  while (start < end && s[start] === '_') start++;
  while (end > start && s[end - 1] === '_') end--;
  return s.slice(start, end);
}

export interface FetchDefFields {
  /** Normalized slug (slugifyName output). */
  name: string;
  url: string;
  kind: FetchKind;
  /** Path segments; [] for plain defs and for root-scalar json picks. */
  path: string[];
  /** '' = no auth. Must name a stored key at fetch time (dangling fails closed). */
  keyLabel: string;
}

/** field -> human message; empty object = valid. Keys match form field names. */
export type FetchDefErrors = Partial<Record<'name' | 'url' | 'kind' | 'path' | 'key_label', string>>;

/**
 * Dotted token form of a path ('forecast.current.temp_f', array indices as
 * bare digits) — the exact spelling inside `{urlfetch:name.<path>}` that the
 * Go dot-path extractor reads.
 */
export function buildJsonPath(segments: string[]): string {
  return segments.join('.');
}

/**
 * Inverse of buildJsonPath, validated against the resolver grammar. Returns
 * null when any segment is malformed so callers can reject instead of storing
 * a path the engine would silently misread.
 */
export function parseJsonPath(dotted: string): string[] | null {
  if (dotted === '') return [];
  const segments = dotted.split('.');
  if (segments.some((s) => !PATH_SEGMENT_RE.test(s))) return null;
  return segments;
}

/**
 * Distinct `{urlfetch:<payload>}` payloads in first-appearance order — the
 * byte-for-byte twin of sesame's urlFetchNames scan (fast-path Contains, then
 * Index('{urlfetch:') / IndexByte('}')). Payloads fold to lower-case because
 * def names are stored bare/lower-case; repeats collapse so one definition
 * referenced three times still costs one fetch, exactly as the engine dedupes.
 */
export function urlFetchNames(response: string): string[] {
  if (!response.includes('{urlfetch')) return [];
  const out: string[] = [];
  const seen = new Set<string>();
  for (const s of fetchSpans(response).spans) {
    const name = (s.payload ?? '').toLowerCase();
    if (name !== '' && !seen.has(name)) {
      seen.add(name);
      out.push(name);
    }
  }
  return out;
}

/**
 * `{urlfetch…` spans that can never resolve — unclosed brace, empty payload,
 * or a payload failing the name/path grammar. The source view flags these
 * verbatim (mark.unknown treatment): typos stay visible, matching the
 * engine's leave-unknown-tokens-literal rule.
 */
interface FetchSpan {
  span: string;
  /** Text after the first ':' inside the braces; null when the token has none. */
  payload: string | null;
}

// fetchSpans walks every '{urlfetch'-prefixed span; dangling carries an
// unclosed trailing token verbatim.
function fetchSpans(response: string): { spans: FetchSpan[]; dangling: string | null } {
  const spans: FetchSpan[] = [];
  let i = response.indexOf('{urlfetch');
  while (i >= 0) {
    const end = response.indexOf('}', i + 1);
    if (end < 0) return { spans, dangling: response.slice(i) };
    const span = response.slice(i, end + 1);
    const body = span.slice(1, -1);
    const colon = body.indexOf(':');
    spans.push({ span, payload: colon < 0 ? null : body.slice(colon + 1) });
    i = response.indexOf('{urlfetch', end);
  }
  return { spans, dangling: null };
}

function malformedFetchPayload(payload: string | null): boolean {
  if (payload === null || payload === '') return true;
  if (payload.includes(':')) return true;
  return parseJsonPath(payload.toLowerCase()) === null;
}

export function malformedUrlFetchTokens(response: string): string[] {
  const { spans, dangling } = fetchSpans(response);
  const bad = spans.filter((s) => malformedFetchPayload(s.payload)).map((s) => s.span);
  if (dangling !== null) bad.push(dangling);
  return bad;
}

/** Hostname shape-check mirroring the Go denylist for author feedback:
 * IP literals (dotted quad or any ':'-bearing IPv6 form), localhost and the
 * .local/.internal suffixes are rejected at save AND fetch time server-side;
 * this browser-side mirror exists because the shared module runs in the
 * browser too and must not import node's net. The Go gate stays authoritative
 * (it also re-checks DNS-era changes and the IP-logger floor). */
function hostIsDenied(host: string): boolean {
  const bare = host.replace(/^\[/, '').replace(/\]$/, '');
  return deniedName(bare) || ipLiteral(bare);
}

function deniedName(bare: string): boolean {
  if (bare === 'localhost') return true;
  return bare.endsWith('.local') || bare.endsWith('.internal');
}

function ipLiteral(bare: string): boolean {
  if (bare.includes(':')) return true; // IPv6 literal form
  return /^(\d{1,3}\.){3}\d{1,3}$/.test(bare); // IPv4 literal form
}

// Each field's rules live in their own problem function returning the first
// violation (or undefined), so the assembler below spends exactly one branch
// per field — the gate shape this file's validators share with validateCommand.
function fetchNameProblem(name: string): string | undefined {
  if (!name) return 'Definition name is required.';
  if (name.length > FETCH_NAME_MAX) return `Definition name must be at most ${FETCH_NAME_MAX} characters.`;
  if (!FETCH_NAME_RE.test(name)) return 'Use lower-case letters, digits and underscores only.';
  return undefined;
}

function parsedUrl(url: string): URL | null {
  try {
    return new URL(url);
  } catch {
    return null;
  }
}

function parsedHttpsUrl(url: string): URL | null {
  const parsed = parsedUrl(url);
  if (!parsed) return null;
  if (parsed.protocol !== 'https:') return null;
  if (!parsed.hostname) return null;
  return parsed;
}

function fetchUrlProblem(url: string): string | undefined {
  if (!url) return 'URL is required.';
  if (url.length > FETCH_URL_MAX) return `URL must be at most ${FETCH_URL_MAX} characters.`;
  const parsed = parsedHttpsUrl(url);
  if (!parsed) return 'URL must start with https://';
  if (hostIsDenied(parsed.hostname)) return 'URL must point at a public https host.';
  return undefined;
}

// kind and path validate together (which field errs depends on the kind), so
// the problem carries its own field name.
function kindPathProblem(f: FetchDefFields): { field: 'kind' | 'path'; msg: string } | undefined {
  if (f.kind !== 'plain' && f.kind !== 'json') return { field: 'kind', msg: 'Pick plain or json.' };
  if (f.kind === 'plain') {
    if (f.path.length === 0) return undefined;
    return { field: 'path', msg: 'A plain fetch reads the whole body — clear the path or switch to json.' };
  }
  return jsonPathProblem(f.path);
}

function jsonPathProblem(path: string[]): { field: 'path'; msg: string } | undefined {
  if (path.length > JSON_PATH_MAX_DEPTH) {
    return { field: 'path', msg: `Path can be at most ${JSON_PATH_MAX_DEPTH} segments deep.` };
  }
  const bad = path.find((s) => !PATH_SEGMENT_RE.test(s));
  if (bad === undefined) return undefined;
  return { field: 'path', msg: `"${bad}" cannot be used as a path segment — letters, digits, "-" and "_" only.` };
}

export function validateFetchDef(f: FetchDefFields): FetchDefErrors {
  const errors: FetchDefErrors = {};
  const name = fetchNameProblem(f.name);
  if (name !== undefined) errors.name = name;
  const url = fetchUrlProblem(f.url);
  if (url !== undefined) errors.url = url;
  const kindPath = kindPathProblem(f);
  if (kindPath !== undefined) errors[kindPath.field] = kindPath.msg;
  if (f.keyLabel.length > KEY_LABEL_MAX) errors.key_label = `Key label must be at most ${KEY_LABEL_MAX} characters.`;
  return errors;
}

/** The bare command trigger: drop a leading "!" and lower-case. */
export function normName(s: string): string {
  return s.trim().replace(/^!+/, '').trim().toLowerCase();
}

/**
 * The response's meaningful lines, mirroring the commands service's
 * normalization: CRLF folds to LF, trailing whitespace per line and blank
 * lines are dropped. Shared by the validator, the editor's counters and the
 * chat rehearsal so all three agree on what actually gets sent.
 */
export function responseLines(response: string): string[] {
  return response
    .split(/\r\n|\r|\n/)
    .map(trimLineEnd)
    .filter((l) => l !== '');
}

/** Canonical wire/storage form: one non-empty chat message per LF-delimited line. */
export function normalizeCommandResponse(response: string): string {
  return responseLines(response).join('\n');
}

// Linear-time right-trim of spaces/tabs (mirrors Go's TrimRight(" \t")); a
// trailing-whitespace regex backtracks polynomially on adversarial input.
function isLineTrailer(ch: string): boolean {
  return ch === ' ' || ch === '\t';
}

function trimLineEnd(line: string): string {
  let end = line.length;
  while (end > 0 && isLineTrailer(line[end - 1])) end--;
  return line.slice(0, end);
}

export interface CommandFields {
  /** Normalized (normName) trigger. */
  name: string;
  /** Normalized, de-duplicated alternate names. */
  aliases: string[];
  response: string;
  cooldown: number;
  /** Digits-only Twitch user id, or '' for unrestricted. */
  allowedUserId: string;
}

/** field -> human message; empty object = valid. Keys match form field names. */
export type CommandErrors = Partial<
  Record<'name' | 'aliases' | 'response' | 'cooldown' | 'allowed_user_id', string>
>;

function nameProblem(name: string, what: string): string | undefined {
  if (!name) return `${what} is required.`;
  if (name.length > COMMAND_NAME_MAX) return `${what} must be at most ${COMMAND_NAME_MAX} characters.`;
  if (/\s/.test(name)) return `${what} cannot contain spaces.`;
  if (name.includes('!')) return `${what} only carries the "!" in chat — leave it out here.`;
  return undefined;
}

// Per-field problem functions, the validateFetchDef shape: each owns one
// field's rules and returns the first violation, so the assembler spends one
// branch per field.
function aliasesProblem(name: string, aliases: string[]): string | undefined {
  const seen = new Set<string>([name]);
  for (const a of aliases) {
    const aliasErr = nameProblem(a, `Alternate name "${a}"`);
    if (aliasErr) return aliasErr;
    if (a === name) return `"${a}" is already the command's own name.`;
    if (seen.has(a)) return `"${a}" is listed twice.`;
    seen.add(a);
  }
  return undefined;
}

function responseProblem(response: string): string | undefined {
  const lines = responseLines(response);
  if (lines.length === 0) return 'Response is required.';
  if (lines.length > RESPONSE_MAX_LINES) {
    return `Response can be at most ${RESPONSE_MAX_LINES} lines — each line is sent as its own chat message.`;
  }
  if (lines.some((l) => l.length > RESPONSE_MAX)) return `Each line must be at most ${RESPONSE_MAX} characters.`;
  if (lines.some(hasControlCharacter)) return 'Response cannot contain control characters.';
  if (urlFetchNames(response).length > URLFETCH_TOKEN_CAP) {
    // Distinct names, not occurrences: the engine dedupes repeats before the
    // fan-out, so the latency budget it must absorb scales with distinct defs.
    return `A response can reference at most ${URLFETCH_TOKEN_CAP} different fetched values ({urlfetch:…}).`;
  }
  return undefined;
}

function outsideCooldownRange(cooldown: number): boolean {
  if (!Number.isFinite(cooldown)) return true;
  return cooldown < 0 || cooldown > COOLDOWN_MAX;
}

function cooldownProblem(cooldown: number): string | undefined {
  if (outsideCooldownRange(cooldown)) return `Cooldown must be between 0 and ${COOLDOWN_MAX} seconds.`;
  if (!Number.isInteger(cooldown)) return 'Cooldown must be a whole number of seconds.';
  return undefined;
}

export function validateCommand(f: CommandFields): CommandErrors {
  const errors: CommandErrors = {};
  const name = nameProblem(f.name, 'Command name');
  if (name) errors.name = name;
  const aliases = aliasesProblem(f.name, f.aliases);
  if (aliases) errors.aliases = aliases;
  const response = responseProblem(f.response);
  if (response) errors.response = response;
  const cooldown = cooldownProblem(f.cooldown);
  if (cooldown) errors.cooldown = cooldown;
  if (f.allowedUserId && !/^[0-9]+$/.test(f.allowedUserId)) {
    errors.allowed_user_id = 'User restriction must be a numeric Twitch user id.';
  }
  return errors;
}

function hasControlCharacter(line: string): boolean {
  for (const char of line) {
    if (char.codePointAt(0)! < 0x20) return true;
  }
  return false;
}

/** Convenience: the first message of an error map, for single-line surfaces. */
export function firstError(errors: CommandErrors | FetchDefErrors): string | undefined {
  return Object.values(errors)[0];
}
