// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// StreamElements config-import source, ported one-for-one from
// app/importer/source/streamelements (streamelements.go + variables.go) when
// the standalone importer service was folded into the dashboard. Fetches a
// channel's custom commands and timers from the StreamElements kappa v2 API
// and translates them into the canonical ImportManifest.
//
// Wire shapes were verified against the public OpenAPI mirrors
// (github.com/api-evangelist/streamelements) and the variable surface against
// https://docs.streamelements.com/chatbot/variables/cheat-sheet. Credential is
// the user's StreamElements JWT from streamelements.com/dashboard/account/
// channels ("Show secrets"), sent as a Bearer token.
//
// Parity contract: pinned against the Go parser's committed golden fixture by
// streamelements.test.ts (testdata/se-golden.json, decoded from the Go suite's
// golden.txt during the port). Diagnostic MESSAGE prose embeds Go %q/%v
// formatting; %q is reproduced via JSON.stringify, which coincides for every
// printable-ASCII input the fixtures carry.

import {
  CODE,
  canonicalizeResponse,
  clampCooldown,
  mapPermission,
  normalizeName
} from './validate';
import type { ImportDiagnostic, ImportManifest, ManifestCommand } from './types';

const warnDiag = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'warn',
  item_index,
  code,
  message
});
const errAt = (item_index: number, code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index,
  code,
  message
});

// --- fetch -------------------------------------------------------------------

// defaultAPIBase is StreamElements' production root; every kappa v2 path is
// appended below it. Injectable so tests point fetchStreamElements at a local
// server.
export const DEFAULT_API_BASE = 'https://api.streamelements.com';

// FETCH_TIMEOUT_MS bounds each upstream call via AbortController. Three
// sequential calls happen per fetch, so worst case is ~30s.
export const FETCH_TIMEOUT_MS = 10_000;

// MAX_RESPONSE_BODY caps how much of one upstream reply is read into memory.
// 16 MiB is orders of magnitude past the largest observed command lists while
// still bounding a hostile or broken server response.
const MAX_RESPONSE_BODY = 16 << 20;

// MAX_CREDENTIAL_LEN: StreamElements channel JWTs run ~700-900 chars today;
// 4096 leaves room for future claims without letting a pasted novel reach the
// transport. Same gate the form action runs client-side; kept here so every
// caller sits behind one gate.
export const MAX_CREDENTIAL_LEN = 4096;

// JWT_SHAPE is three dot-separated base64url segments
// (header.payload.signature). Anything carrying interior whitespace, CR/LF or
// quotes is a paste accident or header-injection bait; failing here returns a
// readable error instead of a transport one.
const JWT_SHAPE = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/;

export class StreamElementsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'StreamElementsError';
  }
}

export interface FetchOptions {
  baseUrl?: string;
  timeoutMs?: number;
}

export interface SeEnvelope {
  commands: unknown[];
  timers: unknown[];
}

// fetchStreamElements resolves the channel's config over the kappa v2 API:
// resolve channelId via /kappa/v2/channels/me, then read the bot collections
// — the flow their API docs describe for every bot endpoint.
export async function fetchStreamElements(
  credential: string,
  opts: FetchOptions = {}
): Promise<SeEnvelope> {
  const token = credential.trim();
  if (token === '')
    throw new StreamElementsError(
      'streamelements: credential is required (JWT from streamelements.com/dashboard/account/channels, "Show secrets")'
    );
  if (token.length > MAX_CREDENTIAL_LEN || !JWT_SHAPE.test(token))
    throw new StreamElementsError(
      'streamelements: credential does not look like a StreamElements JWT (expected eyX.yyy.zzz from "Show secrets")'
    );

  const base = opts.baseUrl || DEFAULT_API_BASE;
  const me = await getJSON<Record<string, unknown>>(base, token, '/kappa/v2/channels/me', opts);
  const id = typeof me._id === 'string' ? me._id : '';
  if (!id)
    throw new StreamElementsError(
      'streamelements: /channels/me returned no _id; the token is not a channel secret JWT'
    );

  const cmds = await getJSON<unknown[]>(base, token, `/kappa/v2/bot/commands/${id}`, opts);
  const timers = await getJSON<unknown[]>(base, token, `/kappa/v2/bot/timers/${id}`, opts);
  return { commands: cmds, timers };
}

async function getJSON<T>(base: string, token: string, path: string, opts: FetchOptions): Promise<T> {
  const timeoutMs = opts.timeoutMs ?? FETCH_TIMEOUT_MS;
  // AbortController bounds the call the way Go's http.Client.Timeout did.
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), timeoutMs);
  try {
    const res = await fetch(base + path, {
      method: 'GET',
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' },
      signal: abort.signal
    });
    // Cap how much of a hostile reply is buffered before decoding.
    const text = await readCapped(res, MAX_RESPONSE_BODY, path);
    if (res.status !== 200)
      throw new StreamElementsError(`${path} returned ${res.status}: ${snippet(text)}${authHint(res.status)}`);
    try {
      return JSON.parse(text) as T;
    } catch (err) {
      throw new StreamElementsError(`${path}: decoding response: ${(err as Error).message}`);
    }
  } catch (err) {
    if (err instanceof StreamElementsError) throw err;
    const reason =
      err instanceof Error && err.name === 'AbortError' ? 'context deadline exceeded' : String(err);
    throw new StreamElementsError(`${path}: ${reason}`);
  } finally {
    clearTimeout(timer);
  }
}

// readCapped reads the body but refuses to buffer more than cap bytes — the
// port of Go's io.LimitReader + oversize rejection. A hostile server streaming
// forever must not balloon the dashboard pod's memory.
async function readCapped(res: Response, cap: number, path: string): Promise<string> {
  try {
    const reader = res.body?.getReader();
    if (!reader) return await res.text();
    const chunks: Uint8Array[] = [];
    let total = 0;
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > cap) {
        await reader.cancel();
        throw new StreamElementsError(`${path}: reading response: body exceeds ${cap} bytes`);
      }
      chunks.push(value);
    }
    const merged = new Uint8Array(total);
    let at = 0;
    for (const c of chunks) {
      merged.set(c, at);
      at += c.byteLength;
    }
    return new TextDecoder().decode(merged);
  } catch (err) {
    if (err instanceof StreamElementsError) throw err;
    throw new StreamElementsError(`${path}: reading response: ${String(err)}`);
  }
}

// authHint appends remediation only where the cause is almost certainly the
// credential: StreamElements JWTs expire, and a stale paste reads identical to
// a revoked one without this nudge.
function authHint(status: number): string {
  return status === 401 || status === 403
    ? ' (re-copy the JWT from streamelements.com/dashboard/account/channels, "Show secrets" — tokens expire)'
    : '';
}

// snippet collapses an upstream error body into one short single-line fragment.
// maxBodySnippet is enough to carry their {"statusCode",...,"message"} JSON,
// short enough that a huge HTML error page cannot flood the preview screen.
const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}

// --- detect ------------------------------------------------------------------

// detectStreamElements reports whether raw is a StreamElements fetch envelope:
// the {commands,timers} shape with at least one recognizably SE-shaped entry,
// so unrelated bots that also happen to expose a "commands" array (Fossabot's
// export JSON, for one) do not steal detection. SE BotCommand entries always
// pair string command+reply with a numeric accessLevel; SE timers uniquely
// carry the chatLines/online window fields.
export function detectStreamElements(raw: Uint8Array | string): boolean {
  const text = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
  if (text.trim().length === 0) return false;
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch {
    return false;
  }
  if (doc === null || typeof doc !== 'object') return false;
  const env = doc as Record<string, unknown>;
  const commands = env.commands;
  const timers = env.timers;
  if (commands !== undefined && !Array.isArray(commands)) return false;
  if (timers !== undefined && !Array.isArray(timers)) return false;
  for (const c of commands ?? []) if (looksLikeCommand(c)) return true;
  for (const t of timers ?? []) if (looksLikeTimer(t)) return true;
  return false;
}

// looksLikeCommand checks one envelope entry against the BotCommand schema's
// distinctive triple: command and reply are strings, accessLevel a number.
function looksLikeCommand(entry: unknown): boolean {
  if (entry === null || typeof entry !== 'object') return false;
  const c = entry as Record<string, unknown>;
  return typeof c.command === 'string' && typeof c.reply === 'string' && typeof c.accessLevel === 'number';
}

// looksLikeTimer checks one envelope entry for the timer schema's unique
// fields: the chatLines gate or the online/offline window objects.
function looksLikeTimer(entry: unknown): boolean {
  if (entry === null || typeof entry !== 'object') return false;
  const t = entry as Record<string, unknown>;
  return t.chatLines !== undefined || t.online !== undefined || t.offline !== undefined;
}

// --- parse -------------------------------------------------------------------

interface BotCommand {
  command: string;
  regex: string;
  reply: string;
  aliases: string[];
  keywords: string[];
  cooldownUser: number;
  cooldownGlobal: number;
  type: string;
  accessLevel: number;
  cost: number;
  enabled: boolean | undefined; // schema default true: undefined means enabled
  enabledOnline: boolean | undefined;
  enabledOffline: boolean | undefined;
}

// decodeBotCommand mirrors encoding/json's struct decode. Field-shape errors
// throw; the caller reports the entry as skipped. (Go's json error strings for
// mismatched types are runtime-specific and NOT reproduced — documented
// divergence, exercised by no committed fixture.)
function decodeBotCommand(entry: unknown): BotCommand {
  if (entry === null || typeof entry !== 'object')
    throw new TypeError('command entry must be an object');
  const e = entry as Record<string, unknown>;
  const str = (v: unknown): string => (typeof v === 'string' ? v : '');
  const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
  const boolUndef = (v: unknown): boolean | undefined => (typeof v === 'boolean' ? v : undefined);
  const cooldown = (e.cooldown ?? {}) as Record<string, unknown>;
  return {
    command: str(e.command),
    regex: str(e.regex),
    reply: str(e.reply),
    aliases: Array.isArray(e.aliases) ? e.aliases.filter((a): a is string => typeof a === 'string') : [],
    keywords: Array.isArray(e.keywords) ? e.keywords.filter((k): k is string => typeof k === 'string') : [],
    cooldownUser: num(cooldown.user),
    cooldownGlobal: num(cooldown.global),
    type: str(e.type),
    accessLevel: num(e.accessLevel),
    cost: num(e.cost),
    enabled: boolUndef(e.enabled),
    enabledOnline: boolUndef(e.enabledOnline),
    enabledOffline: boolUndef(e.enabledOffline)
  };
}

// flexText accepts every message shape observed in timer payloads: a plain
// string or an array of strings / {text} objects (the dashboard writes the
// rotating-message variant). Joined with newlines so CanonicalizeResponse sees
// the same line structure upstream did. Error strings mirror Go's verbatim —
// they surface in the *_skipped diagnostic prose.
function flexText(v: unknown): string {
  if (v === undefined || v === null) return '';
  if (typeof v === 'string') return v;
  if (Array.isArray(v)) return v.map(textElement).join('\n');
  throw new TypeError('timer message must be a string or an array of strings/{text} objects');
}

function textElement(el: unknown): string {
  if (typeof el === 'string') return el;
  if (el !== null && typeof el === 'object' && typeof (el as Record<string, unknown>).text === 'string') {
    return (el as Record<string, unknown>).text as string;
  }
  throw new TypeError('timer message array elements must be strings or {text} objects');
}

interface BotTimer {
  name: string;
  text: string;
  enabled: boolean | undefined;
  onlineEnabled: boolean | undefined;
  onlineInterval: number;
  offlineEnabled: boolean | undefined;
  offlineInterval: number;
}

function decodeBotTimer(entry: unknown): BotTimer {
  if (entry === null || typeof entry !== 'object') throw new TypeError('timer entry must be an object');
  const e = entry as Record<string, unknown>;
  const window = (v: unknown): { enabled: boolean | undefined; interval: number } => {
    if (v === null || typeof v !== 'object') return { enabled: undefined, interval: 0 };
    const w = v as Record<string, unknown>;
    return {
      enabled: typeof w.enabled === 'boolean' ? w.enabled : undefined,
      interval: typeof w.interval === 'number' && Number.isFinite(w.interval) ? w.interval : 0
    };
  };
  const online = window(e.online);
  const offline = window(e.offline);

  return {
    name: typeof e.name === 'string' ? e.name : '',
    text: timerText(e),
    enabled: typeof e.enabled === 'boolean' ? e.enabled : undefined,
    onlineEnabled: online.enabled,
    onlineInterval: online.interval,
    offlineEnabled: offline.enabled,
    offlineInterval: offline.interval
  };
}

// timerText prefers the messages[] shape when present, falling back to message.
function timerText(e: Record<string, unknown>): string {
  if (e.messages === undefined) return flexText(e.message);
  if (!Array.isArray(e.messages)) throw new TypeError('timer message must be a string or an array of strings/{text} objects');
  return e.messages.map((m) => flexText(m)).join('\n');
}

// Diagnostic codes this parser emits beyond the shared consts. Free-form
// snake_case, prefixed with the item kind; the *_skipped family marks source
// items intentionally left out of the manifest, attributed to index -1 (the
// item owns no manifest slot) and always naming the offending item instead.
export const SE_CODE = {
  commandRegexSkipped: 'command_regex_skipped',
  commandDisabledSkipped: 'command_disabled_skipped',
  commandUnparseable: 'command_unparseable_skipped',
  commandAliasDropped: 'command_alias_dropped',
  commandCostUnsupported: 'command_cost_unsupported',
  commandTypeWhisper: 'command_type_whisper',
  commandTypeReply: 'command_type_reply',
  commandTypeUnknown: 'command_type_unknown',
  commandUserCooldownDropped: 'command_user_cooldown_dropped',
  commandOfflineOnlyWidened: 'command_offline_only_widened',
  timerDisabledSkipped: 'timer_disabled_skipped',
  timerUnparseable: 'timer_unparseable_skipped',
  timerOfflineOnlyWidened: 'timer_offline_only_widened',
  timerMessageTruncated: 'timer_message_truncated',
  timerLineDropped: 'timer_message_line_dropped',
  triggerInvalidSkipped: 'trigger_invalid_skipped',
  triggerResponseTruncated: 'trigger_response_truncated',
  triggerResponseLineDropped: 'trigger_response_line_dropped',
  triggerVariableUnmapped: 'trigger_variable_unmapped',
  timerVariableUnmapped: 'timer_variable_unmapped'
} as const;

// accessLevel levels per StreamElements' own documentation, which states these
// seven values are the only ones the bot accepts.
//
// Decision record — accessLevel → perm tier:
//
//	100 Everyone         → everyone    (direct)
//	250 Subscriber       → sub         (direct)
//	300 Regular          → everyone + permission_unmapped warn
//	400 VIP              → vip         (direct)
//	500 Moderator        → mod         (direct)
//	1000 Super Moderator → lead_mod
//	1500 Broadcaster     → broadcaster (direct)
//	anything else        → everyone + permission_unmapped warn
//
// Regular widens to everyone because this bot has no regular tier and the
// mapping layer's documented fallback is widening with a warning. Super
// Moderator lands on lead_mod rather than mod: SE super mods are a
// manually-assigned trust tier strictly between mod and broadcaster, which is
// exactly the niche lead_mod occupies here. Unknown numerics cannot be trusted
// as "more than everyone" — inventing trust from an undocumented value is the
// one unrecoverable mistake a permission mapper can make.
const ACCESS_LEVELS: readonly {
  level: number;
  perm: 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster';
  label: string;
  recognized: boolean;
}[] = [
  { level: 100, perm: 'everyone', label: 'Everyone', recognized: true },
  { level: 250, perm: 'sub', label: 'Subscriber', recognized: true },
  { level: 300, perm: 'everyone', label: 'Regular', recognized: false },
  { level: 400, perm: 'vip', label: 'VIP', recognized: true },
  { level: 500, perm: 'mod', label: 'Moderator', recognized: true },
  { level: 1000, perm: 'lead_mod', label: 'Super Moderator', recognized: true },
  { level: 1500, perm: 'broadcaster', label: 'Broadcaster', recognized: true }
];

export function mapAccessLevel(level: number): { perm: 'everyone' | 'sub' | 'vip' | 'mod' | 'lead_mod' | 'broadcaster'; recognized: boolean } {
  const row = ACCESS_LEVELS.find((r) => r.level === level);
  return row ? { perm: row.perm, recognized: row.recognized } : { perm: 'everyone', recognized: false };
}

// levelLabel renders SE's own names inside warning messages so the broadcaster
// sees their dashboard vocabulary, not ours.
function levelLabel(level: number): string {
  return ACCESS_LEVELS.find((r) => r.level === level)?.label ?? `unknown level ${level}`;
}

const q = (s: string): string => JSON.stringify(s);

// Parse translates a fetched envelope into the canonical manifest. It never
// pre-filters on collisions (the service layer owns those) and reports every
// lossy translation as a warn diagnostic; items that cannot land at all are
// either excluded outright (regex commands, disabled items — with a *_skipped
// warn naming them) or carried with an error diagnostic commit drops.
export function parseStreamElements(raw: Uint8Array | string): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const text = typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch (err) {
    throw new Error(`streamelements: payload is not a commands/timers envelope: ${(err as Error).message}`);
  }
  if (doc === null || typeof doc !== 'object' || Array.isArray(doc))
    throw new Error('streamelements: payload is not a commands/timers envelope: JSON must be an object');
  const env = doc as Record<string, unknown>;
  if (env.commands !== undefined && !Array.isArray(env.commands))
    throw new Error('streamelements: payload is not a commands/timers envelope: commands must be an array');
  if (env.timers !== undefined && !Array.isArray(env.timers))
    throw new Error('streamelements: payload is not a commands/timers envelope: timers must be an array');

  const manifest: ImportManifest = {};
  const diags: ImportDiagnostic[] = [];

  // Empty collections are deleted so the serialized shape mirrors the Go
  // struct's omitempty tags exactly.
  manifest.commands = [];
  parseCommands(env.commands ?? [], manifest.commands, manifest, diags);
  if (manifest.commands.length === 0) delete manifest.commands;

  manifest.timers = [];
  parseTimers(env.timers ?? [], manifest.timers, diags);
  if (manifest.timers.length === 0) delete manifest.timers;

  return { manifest, diagnostics: diags };
}

// NoteSink accumulates the per-item warn diagnostics that also surface on the
// command as warnings: every lossy note is both a diagnostic at idx and a
// message in cmd.warnings.
interface NoteSink {
  idx: number;
  diags: ImportDiagnostic[];
  notes: string[];
}

function addNote(sink: NoteSink, code: string, message: string): void {
  sink.diags.push(warnDiag(sink.idx, code, message));
  sink.notes.push(message);
}

function parseCommands(entries: unknown[], commands: ManifestCommand[], m: ImportManifest, diags: ImportDiagnostic[]): void {
  for (const entry of entries) {
    const c = decodeCommandOrSkip(entry, diags);
    if (!c) continue;
    const problem = commandExclusion(c);
    if (problem) {
      diags.push(problem);
      continue;
    }
    appendCommand(c, commands, m, diags);
  }
}

function decodeCommandOrSkip(entry: unknown, diags: ImportDiagnostic[]): BotCommand | null {
  try {
    return decodeBotCommand(entry);
  } catch (err) {
    diags.push(warnDiag(-1, SE_CODE.commandUnparseable, 'skipped one unparseable StreamElements command entry: ' + (err as Error).message));
    return null;
  }
}

// commandExclusion reports why an entry cannot land at all (attributed to
// index -1: it owns no manifest slot), or null when it should be imported.
//
// Regex commands are a different feature (pattern-triggered), not a
// command with an unlucky name; importing one would ship a command whose
// trigger is a regex literal. Excluded, not errored-in-place, so the
// preview never offers a broken item for confirmation.
function commandExclusion(c: BotCommand): ImportDiagnostic | null {
  if (c.regex.trim() !== '') {
    return warnDiag(-1, SE_CODE.commandRegexSkipped,
      `command ${q(c.command)} uses a regex trigger (${c.regex}); regex commands have no equivalent here and were skipped`);
  }
  if (!flag(c.enabled)) {
    return warnDiag(-1, SE_CODE.commandDisabledSkipped,
      `command ${q(c.command)} is disabled upstream and was skipped`);
  }
  if (!flag(c.enabledOnline) && !flag(c.enabledOffline)) {
    return warnDiag(-1, SE_CODE.commandDisabledSkipped,
      `command ${q(c.command)} is disabled both online and offline upstream and was skipped`);
  }
  return null;
}

function appendCommand(c: BotCommand, commands: ManifestCommand[], m: ImportManifest, diags: ImportDiagnostic[]): void {
  const name = normalizeName(c.command);
  const sink: NoteSink = { idx: commands.length, diags, notes: [] };
  const online = flag(c.enabledOnline);

  const { text, perm } = lossyNotes(c, name, online, sink);
  const { lines, diags: respDiags } = canonicalizeResponse(text, sink.idx);

  commands.push(assembleCommand(c, name, online, perm, lines, sink));
  diags.push(...respDiags);
  emptyResponseErrors(lines, c, name, sink.idx, diags);
  keywordsToTriggers(c.keywords, c.reply, name, m, diags);
}

// lossyNotes emits, in pinned order (the golden fixtures pin diagnostic
// order), every widening/dropping translation note for one command and sinks
// the variable-translation warnings. Returns the translated response text and
// the resolved permission tier.
type AccessTier = ReturnType<typeof mapAccessLevel>['perm'];

function lossyNotes(c: BotCommand, name: string, online: boolean, sink: NoteSink): { text: string; perm: AccessTier } {
  if (!online && flag(c.enabledOffline)) {
    addNote(sink, SE_CODE.commandOfflineOnlyWidened,
      `command ${q(name)} runs only while offline upstream; imported as always available (widening)`);
  }

  responseTypeNote(c, name, sink);

  if (c.cost > 0) {
    addNote(sink, SE_CODE.commandCostUnsupported,
      `command ${q(name)} costs ${c.cost} loyalty points upstream; loyalty gating is not supported, so it is free here`);
  }

  const perm = accessLevelNote(c, name, sink).perm;

  if (c.cooldownUser > 0) {
    addNote(sink, SE_CODE.commandUserCooldownDropped,
      `command ${q(name)} had a ${c.cooldownUser}s per-user cooldown; only the shared cooldown (${c.cooldownGlobal}s) is kept`);
  }

  const { text, warns } = translateVariables(c.reply);
  for (const tok of warns) {
    addNote(sink, CODE.variableUnmapped,
      `response uses ${tok}, which has no equivalent; left as literal text`);
  }
  return { text, perm };
}

// responseTypeNote covers the response-style column: say/default posts
// unchanged, reply/whisper lose their delivery style, unknown types post as
// plain chat messages.
function responseTypeNote(c: BotCommand, name: string, sink: NoteSink): void {
  switch (c.type.trim().toLowerCase()) {
    case '':
    case 'say':
      break;
    case 'reply':
      addNote(sink, SE_CODE.commandTypeReply,
        `command ${q(name)} replies natively upstream; it posts as a normal chat message here`);
      break;
    case 'whisper':
      addNote(sink, SE_CODE.commandTypeWhisper,
        `command ${q(name)} whispered its response upstream; the response posts publicly here`);
      break;
    default:
      addNote(sink, SE_CODE.commandTypeUnknown,
        `command ${q(name)} has unknown response type ${q(c.type)}; posting as a normal chat message`);
  }
}

// accessLevelNote warns when the source tier has no equivalent here.
function accessLevelNote(c: BotCommand, name: string, sink: NoteSink): ReturnType<typeof mapAccessLevel> {
  const mapped = mapAccessLevel(c.accessLevel);
  if (!mapped.recognized) {
    addNote(sink, CODE.permissionUnmapped,
      `command ${q(name)} requires accessLevel ${c.accessLevel} (${levelLabel(c.accessLevel)}), which has no equivalent here; widened to everyone`);
  }
  return mapped;
}

// assembleCommand builds the manifest entry with omitempty parity: absent or
// empty values are omitted so a serialized manifest stays identical to what
// the importer service emitted.
function assembleCommand(
  c: BotCommand,
  name: string,
  online: boolean,
  perm: string,
  lines: string[],
  sink: NoteSink
): ManifestCommand {
  const cmd: ManifestCommand = { name, responses: lines };
  const aliases = collectAliases(c, name, sink);
  if (aliases.length > 0) cmd.aliases = aliases;
  cmd.permission = perm as ManifestCommand['permission'];
  if (clampCooldown(c.cooldownGlobal) > 0) cmd.cooldown_seconds = clampCooldown(c.cooldownGlobal);
  if (online && !flag(c.enabledOffline)) cmd.online_only = true;
  if (sink.notes.length > 0) cmd.warnings = sink.notes;
  return cmd;
}

// collectAliases normalizes the alias list, recording a drop note for every
// alias that cannot land. Returns the kept, de-duplicated aliases.
function collectAliases(c: BotCommand, name: string, sink: NoteSink): string[] {
  const aliases: string[] = [];
  for (const a of c.aliases) {
    const norm = normalizeName(a);
    if (norm === '') {
      addNote(sink, SE_CODE.commandAliasDropped,
        `command ${q(name)} had alias ${q(a)} that normalizes to nothing; dropped`);
    } else if (norm === name) {
      addNote(sink, SE_CODE.commandAliasDropped,
        `command ${q(name)} listed itself as an alias; dropped`);
    } else if (!aliases.includes(norm)) {
      aliases.push(norm);
    }
  }
  return aliases;
}

// emptyResponseErrors marks commands whose canonicalization left nothing, so
// commit skips rather than writes an empty command.
function emptyResponseErrors(lines: string[], c: BotCommand, name: string, idx: number, diags: ImportDiagnostic[]): void {
  if (lines.length > 0) return;
  if (c.reply.trim() !== '') {
    // Canonicalization dropped everything (blank lines only after variable
    // removal).
    diags.push(errAt(idx, CODE.responseInvalid, `command ${q(name)} has no usable response after translation`));
  } else {
    diags.push(errAt(idx, CODE.responseInvalid, `command ${q(name)} has no response`));
  }
}

function keywordsToTriggers(keywords: string[], reply: string, commandName: string, m: ImportManifest, diags: ImportDiagnostic[]): void {
  for (const kw of keywords) {
    appendKeywordTrigger(kw, reply, commandName, m, diags);
  }
  if (m.triggers?.length === 0) delete m.triggers;
}

// appendKeywordTrigger expands one keyword into a phrase trigger carrying the
// same translated response, flattened to a single line: commit stores triggers
// as "phrase => response" textarea rows, so embedded newlines would corrupt
// the rules blob.
function appendKeywordTrigger(kw: string, reply: string, commandName: string, m: ImportManifest, diags: ImportDiagnostic[]): void {
  const phrase = kw.trim();
  if (phrase === '') return;

  m.triggers ??= [];
  const idx = m.triggers.length;
  const { text, warns } = translateVariables(reply);
  for (const tok of warns) {
    diags.push(warnDiag(idx, SE_CODE.triggerVariableUnmapped,
      `keyword ${q(phrase)} response uses ${tok}, which has no equivalent; left as literal text`));
  }

  const { lines, diags: respDiags } = canonicalizeResponse(text, idx);
  // CanonicalizeResponse attributes its findings with command_-prefixed
  // codes; these items are triggers, so the codes are re-prefixed to keep
  // FailedItems dropping the right collection.
  retitleResponseCodes(respDiags, SE_CODE.triggerResponseTruncated, SE_CODE.triggerResponseLineDropped);
  diags.push(...respDiags);

  const response = lines.join(' ');
  if (response === '') {
    diags.push(warnDiag(-1, SE_CODE.triggerInvalidSkipped,
      `keyword ${q(kw)} on command ${q(commandName)} has no usable phrase/response pair; skipped`));
    return;
  }
  m.triggers.push({ phrase, response });
}

// retitleResponseCodes re-prefixes canonicalization findings from their
// command_ codes onto the caller's item kind.
function retitleResponseCodes(diags: ImportDiagnostic[], truncatedCode: string, lineDroppedCode: string): void {
  for (const d of diags) {
    if (d.code === CODE.responseTruncated) d.code = truncatedCode;
    else if (d.code === CODE.responseLineDropped) d.code = lineDroppedCode;
  }
}

function parseTimers(entries: unknown[], timers: NonNullable<ImportManifest['timers']>, diags: ImportDiagnostic[]): void {
  for (const entry of entries) {
    const t = decodeTimerOrSkip(entry, diags);
    if (!t) continue;
    const label = timerLabel(t);
    const problem = timerExclusion(t, label);
    if (problem) {
      diags.push(problem);
      continue;
    }
    appendTimer(t, label, timers, diags);
  }
}

function decodeTimerOrSkip(entry: unknown, diags: ImportDiagnostic[]): BotTimer | null {
  try {
    return decodeBotTimer(entry);
  } catch (err) {
    diags.push(warnDiag(-1, SE_CODE.timerUnparseable, 'skipped one unparseable StreamElements timer entry: ' + (err as Error).message));
    return null;
  }
}

function timerLabel(t: BotTimer): string {
  return t.name !== '' ? t.name : firstLine(t.text) || '(unnamed)';
}

// timerExclusion reports why the timer cannot land at all, or null.
function timerExclusion(t: BotTimer, label: string): ImportDiagnostic | null {
  if (!flag(t.enabled)) {
    return warnDiag(-1, SE_CODE.timerDisabledSkipped,
      `timer ${q(label)} is disabled upstream and was skipped`);
  }
  if (!flag(t.onlineEnabled) && !flag(t.offlineEnabled)) {
    return warnDiag(-1, SE_CODE.timerDisabledSkipped,
      `timer ${q(label)} has neither an online nor an offline window enabled upstream and was skipped`);
  }
  return null;
}

function appendTimer(t: BotTimer, label: string, timers: NonNullable<ImportManifest['timers']>, diags: ImportDiagnostic[]): void {
  const idx = timers.length;
  // Decision record — interval units: StreamElements timer intervals are
  // MINUTES. Their dashboard labels the field "Interval (minutes)" and the
  // API's own examples (online 5, offline 30) only make sense on a minute
  // scale — a 5-second repeating announcement would sit below any sane rate
  // limit and below this engine's 30s floor. Multiply by 60 here, once, so
  // the manifest carries seconds like every consumer expects; commit clamps
  // sub-floor values itself.
  const window = timerWindow(t);
  if (window.widened) {
    diags.push(warnDiag(idx, SE_CODE.timerOfflineOnlyWidened,
      `timer ${q(label)} runs only while offline upstream; timers here fire only while live, so it will run while live instead (widening)`));
  }

  const { text, warns } = translateVariables(t.text);
  for (const tok of warns) {
    diags.push(warnDiag(idx, 'timer_variable_unmapped',
      `timer message uses ${tok}, which has no equivalent; left as literal text`));
  }

  const { lines, diags: respDiags } = canonicalizeResponse(text, idx);
  retitleResponseCodes(respDiags, SE_CODE.timerMessageTruncated, SE_CODE.timerLineDropped);
  diags.push(...respDiags);

  // omitempty parity: online_only false is omitted.
  const timer: { message: string; interval_seconds: number; online_only?: boolean } = {
    message: lines.join('\n'),
    interval_seconds: window.seconds
  };
  if (window.onlineOnly) timer.online_only = true;
  timers.push(timer);

  if (timer.message.trim() === '') {
    diags.push(errAt(idx, CODE.timerMessageEmpty, `timer ${q(label)} has no usable message after translation`));
  }
}

// timerWindow resolves which firing window survives the import. Exactly one
// of the two is enabled here (the neither case was excluded); an offline-only
// timer widens with a note because timers here fire only while live.
function timerWindow(t: BotTimer): { seconds: number; onlineOnly: boolean; widened: boolean } {
  const clampNegative = (s: number): number => (s < 0 ? 0 : s);
  if (flag(t.onlineEnabled)) {
    return { seconds: clampNegative(t.onlineInterval * 60), onlineOnly: true, widened: false };
  }
  return { seconds: clampNegative(t.offlineInterval * 60), onlineOnly: false, widened: true };
}

// flag dereferences an optional boolean with the schema's enabled-by-default.
function flag(p: boolean | undefined): boolean {
  return p === undefined || p;
}

// firstLine returns the first non-blank line of s, for labeling unnamed timers.
function firstLine(s: string): string {
  for (const line of s.split('\n')) {
    const t = line.trim();
    if (t !== '') return t;
  }
  return '';
}

// --- variable translation (variables.go) --------------------------------------

// maxPasses bounds the translation loop. One pass rewrites every token it can
// see left-to-right; outer composites that swallowed an inner token come out
// of pass 1 as literal text containing a now-visible token, which pass 2
// translates (e.g. "$(weather ${1:})" becomes "$(weather {args})" plus one
// warn about the composite). Three passes settle any realistically nested
// response while guaranteeing termination.
const MAX_PASSES = 3;

// legacyHeads are the bare-{...} names worth treating as variables. Anything
// else inside plain braces is left completely alone — braces are punctuation.
const LEGACY_HEADS = new Set([
  'user', 'sender', 'source', 'touser', 'target', 'channel',
  'getcount', 'count', 'choose', 'random', 'args'
]);

// translateVariables rewrites StreamElements variable references into this
// bot's single-pass {key} substitution syntax. Both documented delimiter
// styles — $(name) and ${name}, interchangeable upstream — plus the older
// bare-{name} community shorthand are recognized; unknown $()/${} tokens stay
// literal and are reported (the broadcaster clearly attempted a variable),
// while unknown bare braces stay literal silently (braces are ordinary chat
// punctuation, and warning on every stray pair would bury real findings).
//
// Decision record — StreamElements variable table:
//
//	$(user) / ${user} / $(user.name)   → {user}
//	$(sender) / $(source) / .name      → {sender}
//	$(touser) / $(target|.user|.name)  → {touser}
//	$(1:)                              → {args}      words 1..end
//	$(N)/$(N:M)/$(:M)/fallbacks        → literal+warn
//	$(channel|alias|display_name)      → {channel}
//	$(getcount NAME)                   → {counter:name}  (normalized)
//	$(count ...)                       → literal+warn  mutates upstream
//	$(random[.X-Y]|random.number X-Y)  → {random:X-Y}
//	$(random.pick …)/{choose …}        → {choice:a,b}  quote-aware
//	game/title/uptime/if/math/api/…    → literal + warn (no equivalent)
export function translateVariables(inText: string): { text: string; warns: string[] } {
  const seen = new Set<string>();
  const order: string[] = [];

  let s = inText;
  for (let pass = 0; pass < MAX_PASSES; pass++) {
    let b = '';
    let pos = 0;
    let changed = false;

    for (;;) {
      const found = findNext(s, pos);
      if (!found) break;
      b += s.slice(pos, found.start);
      const tok = s.slice(found.start, found.end);
      const { repl, warned } = classifyToken(tok);
      b += repl;
      if (warned && !seen.has(tok)) {
        seen.add(tok);
        order.push(tok);
      }
      if (repl !== tok) changed = true;
      pos = found.end;
    }
    b += s.slice(pos);

    if (!changed) break;
    s = b;
  }
  return { text: s, warns: order };
}

// findNext locates the next translatable token starting at or after from:
// always a $(…)/${…} pair, and a bare {…} pair only when its head is in
// LEGACY_HEADS. Returns null when none remains.
//
// A delimited token whose body nests another explicit token is skipped in
// favour of its interior: the composite has no mapping of its own (it is
// warned as-is next pass, once its interior reads translated), while the inner
// leaf can land cleanly right now. This is what turns SE's documented nesting
// "$(weather ${1:})" into "$(weather {args})" instead of stranding the
// argument untranslated inside a dead wrapper.
function findNext(s: string, from: number): { start: number; end: number } | null {
  for (let i = from; i < s.length; i++) {
    const ch = s.charCodeAt(i);
    if (ch === 0x24 /* $ */ && i + 1 < s.length && (s[i + 1] === '(' || s[i + 1] === '{')) {
      const end = matchDelimited(s, i);
      if (end !== -1) {
        if (!hasNestedExplicit(s, i + 2, end - 1)) return { start: i, end };
        i++; // descend past the wrapper's opener; its closer stays literal
      } else {
        i++; // malformed explicit token: step past '$' and keep scanning
      }
    } else if (ch === 0x7b /* { */) {
      const end = matchBrace(s, i);
      if (end !== -1 && legacyCandidate(s.slice(i + 1, end - 1))) return { start: i, end };
    }
  }
  return null;
}

// hasNestedExplicit reports whether s[from:until] opens another $( or ${
// token, i.e. whether the enclosing token is a composite.
function hasNestedExplicit(s: string, from: number, until: number): boolean {
  for (let j = from; j < until - 1; j++) {
    if (s[j] === '$' && j + 1 < until && (s[j + 1] === '(' || s[j + 1] === '{')) return true;
  }
  return false;
}

// matchDelimited finds the closing half of the $( or ${ opened at start,
// tracking both families' depth so one nested opposite-family token does not
// end the outer scan early. Returns -1 when unbalanced (token left literal).
function matchDelimited(s: string, start: number): number {
  const openParen = s[start + 1] === '(';
  let paren = 0;
  let brace = 0;
  for (let j = start; j < s.length; j++) {
    if (s[j] === '$' && j + 1 < s.length) {
      if (s[j + 1] === '(') {
        paren++;
        j++;
        continue;
      }
      if (s[j + 1] === '{') {
        brace++;
        j++;
        continue;
      }
    }
    switch (s[j]) {
      case '(':
        paren++;
        break;
      case ')':
        paren--;
        if (openParen && paren === 0 && brace <= 0) return j + 1;
        break;
      case '{':
        brace++;
        break;
      case '}':
        brace--;
        if (!openParen && brace === 0 && paren <= 0) return j + 1;
        break;
    }
  }
  return -1;
}

// matchBrace finds the } closing the { at start (nested braces counted);
// -1 when unbalanced.
function matchBrace(s: string, start: number): number {
  let depth = 0;
  for (let j = start; j < s.length; j++) {
    if (s[j] === '{') depth++;
    else if (s[j] === '}') {
      depth--;
      if (depth === 0) return j + 1;
    }
  }
  return -1;
}

// legacyCandidate reports whether a bare-brace body names a variable family we
// recognize. Comparison is ASCII-case-insensitive on the leading name run.
function legacyCandidate(inner: string): boolean {
  const [head] = splitHead(inner.trim());
  return LEGACY_HEADS.has(head);
}

// splitHead splits a token body into its lower-cased name run ([0-9a-z_]+)
// and the remainder (dot-subfields, arguments, ranges, pipes).
function splitHead(body: string): [head: string, rest: string] {
  let i = 0;
  while (i < body.length) {
    const c = body[i];
    if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c === '_') {
      i++;
      continue;
    }
    break;
  }
  return [asciiLower(body.slice(0, i)), body.slice(i)];
}

// ASCII-only lowering: String.toLowerCase() folds Unicode too, and a
// non-ASCII byte must terminate the name run exactly like Go's byte scan.
function asciiLower(s: string): string {
  let hasUpper = false;
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 65 && c <= 90) {
      hasUpper = true;
      break;
    }
  }
  if (!hasUpper) return s;
  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    out += c >= 65 && c <= 90 ? String.fromCharCode(c + 32) : s[i];
  }
  return out;
}

// TokenView carries one scanned token through the rule table below. head is
// the lower-cased name run; restRaw/rest are the remainder in original and
// lower-cased bytes (sub-field suffixes compare case-insensitively so
// $(USER.NAME) resolves like $(user.name), while argument payloads keep their
// original bytes so counter names and choice items survive verbatim);
// delimited says whether an explicit $(…)/${…} wrapper was present.
interface TokenView {
  tok: string;
  head: string;
  restRaw: string;
  rest: string;
  delimited: boolean;
}

type TokenOutcome = { repl: string; warned: boolean };

// classifyToken resolves one scanned token to its replacement text by looking
// its head up in TOKEN_RULES (unknown heads fall through to unmapped).
// Returns warned=true when the token was an attempted-but-unmappable variable;
// false covers both clean translations and silent literals.
function classifyToken(tok: string): TokenOutcome {
  const v = readToken(tok);
  if (!v) return { repl: tok, warned: false };
  return (TOKEN_RULES[v.head] ?? unmapped)(v);
}

function readToken(tok: string): TokenView | null {
  if (isDelimitedToken(tok)) return tokenOf(tok, tok.slice(2, -1), true);
  if (isBareBraceToken(tok)) return tokenOf(tok, tok.slice(1, -1), false);
  return null;
}

function isDelimitedToken(tok: string): boolean {
  return (
    (tok.startsWith('$(') && tok.endsWith(')') && tok.length > 3) ||
    (tok.startsWith('${') && tok.endsWith('}') && tok.length > 3)
  );
}

function isBareBraceToken(tok: string): boolean {
  return tok.startsWith('{') && tok.endsWith('}') && tok.length > 2;
}

function tokenOf(tok: string, inner: string, delimited: boolean): TokenView {
  const [head, restRaw] = splitHead(inner.trim());
  return { tok, head, restRaw, rest: asciiLower(restRaw), delimited };
}

const ok = (repl: string): TokenOutcome => ({ repl, warned: false });
const silentLiteral = (v: TokenView): TokenOutcome => ({ repl: v.tok, warned: false });
const flaggedLiteral = (v: TokenView): TokenOutcome => ({ repl: v.tok, warned: true });

// unmapped keeps an attempted-but-unmappable variable literal. Explicit
// $(…)/${…} attempts warn — someone clearly wrote a variable — while
// bare-brace bodies stay silent except the counter families: bare braces are
// ambiguous punctuation, but rewriting count/getcount would drop their
// increment side-effect.
function unmapped(v: TokenView): TokenOutcome {
  if (v.delimited) return flaggedLiteral(v);
  return { repl: v.tok, warned: v.head === 'count' || v.head === 'getcount' };
}

// bakedIdentity maps one identity family: bare or dot-subfield suffixes land
// on a single canonical key, anything else falls back to unmapped.
function bakedIdentity(repl: string, ...suffixes: string[]): (v: TokenView) => TokenOutcome {
  return (v) => (v.rest === '' || suffixes.includes(v.rest) ? ok(repl) : unmapped(v));
}

// headless separates explicit headless tokens ($(:3)-style ranges and other
// nameless attempts, which warn) from bare nameless braces (punctuation).
function headless(v: TokenView): TokenOutcome {
  return v.delimited && v.rest !== '' ? flaggedLiteral(v) : silentLiteral(v);
}

function touserParam(v: TokenView): TokenOutcome {
  return v.restRaw === '' ? ok('{touser}') : unmapped(v);
}

// argsRange maps $(1:) — words 1..end — to {args}; every other numeric form
// stays put.
function argsRange(v: TokenView): TokenOutcome {
  return v.delimited && v.restRaw === ':' ? ok('{args}') : unmapped(v);
}

function getCounterParam(v: TokenView): TokenOutcome {
  const name = firstWord(v.restRaw);
  if (name === '') return unmapped(v);
  const norm = normalizeName(name);
  return norm !== '' ? ok(`{counter:${norm}}`) : unmapped(v);
}

function chooseParam(v: TokenView): TokenOutcome {
  if (v.delimited) return unmapped(v);
  // The legacy bare {choose a,b,c} form warns on failure like an explicit
  // attempt: someone clearly wrote a choice list.
  const items = pickItems(v.restRaw);
  return items ? ok('{choice:' + items.join(',') + '}') : flaggedLiteral(v);
}

// TOKEN_RULES resolves a token body by its head name. Adding a variable later
// is a row here, not a branch in the scanner.
const TOKEN_RULES: Record<string, (v: TokenView) => TokenOutcome> = {
  '': headless,
  user: bakedIdentity('{user}', '.name'),
  sender: bakedIdentity('{sender}', '.name'),
  source: bakedIdentity('{sender}', '.name'),
  touser: touserParam,
  target: bakedIdentity('{touser}', '.user', '.name'),
  channel: bakedIdentity('{channel}', '.alias', '.display_name'),
  '1': argsRange,
  getcount: getCounterParam,
  // Mutating upstream (increments and returns); our {counter:*} substitution
  // only reads, so the token always stays literal with a warning.
  count: flaggedLiteral,
  random: classifyRandom,
  choose: chooseParam
};

// Each RANDOM_ROWS entry renders one documented $(random …)/{random …}
// spelling, or null when its arguments do not parse; classifyRandom walks the
// rows in order and the first hit wins. Rows are mutually exclusive by prefix,
// mirroring the upstream grammar: bare range, dot-forms ($(random.X)),
// space-pick, plain bare-brace range.
type RandomRow = (v: TokenView) => string | null;

const RANDOM_ROWS: RandomRow[] = [
  (v) => (v.restRaw === '' ? '{random}' : null),
  dotRandomRange,
  dotRandomNumber,
  dotRandomPick,
  spaceRandomPick,
  plainRandomRange
];

// classifyRandom resolves $(random …) forms. Bare-brace random uses a space
// before the range ({random 5-10}); delimited uses a dot ($(random.5-10)).
// An unusable shape stays literal, warning only on the explicit attempt.
function classifyRandom(v: TokenView): TokenOutcome {
  const repl = RANDOM_ROWS.map((row) => row(v)).find((r) => r !== null);
  if (repl !== undefined) return ok(repl);
  return v.delimited ? flaggedLiteral(v) : silentLiteral(v);
}

function dotRandomRange(v: TokenView): string | null {
  if (!v.rest.startsWith('.')) return null;
  const r = parseRange(v.restRaw.slice(1));
  return r ? rangeKey(r[0], r[1]) : null;
}

function dotRandomNumber(v: TokenView): string | null {
  if (!v.rest.startsWith('.number')) return null;
  const fields = v.restRaw.slice(1 + 'number'.length).trim().split(/\s+/).filter(Boolean);
  if (fields.length !== 1) return null;
  const r = parseRange(fields[0]);
  return r ? rangeKey(r[0], r[1]) : null;
}

function dotRandomPick(v: TokenView): string | null {
  if (!v.rest.startsWith('.pick')) return null;
  return pickKey(pickItems(v.restRaw.slice(1 + 'pick'.length)));
}

function spaceRandomPick(v: TokenView): string | null {
  if (!v.rest.startsWith(' pick')) return null;
  return pickKey(pickItems(v.restRaw.slice(' pick'.length)));
}

function plainRandomRange(v: TokenView): string | null {
  // Dot-prefixed rests can never satisfy goAtoi's integer halves (a leading
  // '.', letter or space cannot open an int), so this row only has to exclude
  // the delimited form to match the upstream ladder's reach exactly.
  if (v.delimited || v.rest.startsWith('.')) return null;
  const r = parseRange(v.restRaw);
  return r ? rangeKey(r[0], r[1]) : null;
}

function pickKey(items: string[] | null): string | null {
  return items ? '{choice:' + items.join(',') + '}' : null;
}

// pickItems splits a random.pick argument list honoring quotes: items may be
// wrapped in '…', "…" or `…` to carry spaces, and both space- and comma-
// separated lists are accepted (SE's two documented forms). Any item that
// itself contains a comma after quote-stripping fails, because our {choice:…}
// grammar splits on every comma and cannot represent the difference — leaving
// the token literal beats corrupting the list.
function pickItems(spec: string): string[] | null {
  spec = spec.trim();
  if (spec === '') return null;

  const raw: string[] = [];
  let cur = '';
  const flush = (): void => {
    const t = cur.trim();
    if (t !== '') raw.push(t);
    cur = '';
  };
  let quote = '\0';
  for (let i = 0; i < spec.length; i++) {
    const c = spec[i];
    if (quote !== '\0') {
      if (c === quote) quote = '\0';
      else cur += c;
    } else if (c === "'" || c === '"' || c === '`') {
      quote = c;
    } else if (c === ' ' || c === '\t' || c === ',') {
      flush();
    } else {
      cur += c;
    }
  }
  if (quote !== '\0') return null; // unterminated quote
  flush();

  const items: string[] = [];
  for (const r of raw) {
    let t = r.trim();
    if (t.length >= 2 && (t[0] === "'" || t[0] === '"' || t[0] === '`') && t[t.length - 1] === t[0]) {
      t = t.slice(1, -1).trim();
    }
    if (t === '') continue;
    if (t.includes(',')) return null; // cannot survive our comma-splitting grammar
    items.push(t);
  }
  if (items.length === 0) return null;
  return items;
}

// rangeKey formats a parsed random range canonically.
function rangeKey(x: number, y: number): string {
  return `{random:${x}-${y}}`;
}

// parseRange reads "X-Y" (optionally spaced, signs allowed). The split dash is
// searched right-to-left and the first split where both sides parse as
// integers wins, so negative bounds resolve ("-5--1" → -5, -1) while a plain
// "5-10" still takes its only dash.
function parseRange(s: string): [number, number] | null {
  s = s.trim();
  for (let i = s.length - 2; i > 0; i--) {
    if (s[i] !== '-') continue;
    const x = goAtoi(s.slice(0, i).trim());
    if (x === null) continue;
    const y = goAtoi(s.slice(i + 1).trim());
    if (y !== null) return [x, y];
  }
  return null;
}

// goAtoi mirrors strconv.Atoi: optional sign then digits, nothing else.
function goAtoi(s: string): number | null {
  if (!/^[+-]?\d+$/.test(s)) return null;
  const n = Number(s);
  // Go ints are 64-bit; JSON-scale ranges never overflow Number here.
  return Number.isSafeInteger(n) ? n : null;
}

// firstWord returns the first whitespace-delimited word of s, if any.
function firstWord(s: string): string {
  const f = s.split(/\s+/).filter(Boolean);
  return f.length > 0 ? f[0] : '';
}
