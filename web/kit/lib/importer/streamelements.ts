// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  CODE,
  canonicalizeResponse,
  clampCooldown,
  fetchDefSlug,
  mapPermission,
  normalizeName
} from './validate';
import { emit, POSITIONAL_MAX, positional, slice } from './targets';
import { createdFetchDefMessage } from './nightbot/fetchdefs';
import type { ImportDiagnostic, ImportManifest, ManifestCommand, ManifestFetch } from './types';
import { IMPORT_ITEM_CAPS } from './types';

const IF_TOKEN = /^\$[({]if\b/i;

function unmappedClause(tok: string): string {
  if (IF_TOKEN.test(tok)) {
    return 'whose branch cannot be translated automatically; rewrite it using {if:cond:then:else} (see the variables guide)';
  }
  return 'which has no equivalent; left as literal text';
}

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

export const DEFAULT_API_BASE = 'https://api.streamelements.com';

export const FETCH_TIMEOUT_MS = 10_000;

const MAX_RESPONSE_BODY_BYTES = 16 << 20;

export const MAX_CREDENTIAL_LEN = 4096;

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

  const client: KappaClient = { base: opts.baseUrl || DEFAULT_API_BASE, token, opts };
  const me = await kappaGet<Record<string, unknown>>(client, '/kappa/v2/channels/me');
  const id = typeof me._id === 'string' ? me._id : '';
  if (!id)
    throw new StreamElementsError(
      'streamelements: /channels/me returned no _id; the token is not a channel secret JWT'
    );

  const cmds = await kappaGet<unknown[]>(client, `/kappa/v2/bot/commands/${id}`);
  const timers = await kappaGet<unknown[]>(client, `/kappa/v2/bot/timers/${id}`);
  return { commands: cmds, timers };
}

interface KappaClient {
  base: string;
  token: string;
  opts: FetchOptions;
}

interface KappaRequest {
  url: string;
  token: string;
  path: string;
}

async function kappaGet<T>(client: KappaClient, path: string): Promise<T> {
  const timeoutMs = client.opts.timeoutMs ?? FETCH_TIMEOUT_MS;
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), timeoutMs);
  try {
    const req: KappaRequest = { url: client.base + path, token: client.token, path };
    return await requestJSON<T>(req, abort.signal);
  } catch (err) {
    if (err instanceof StreamElementsError) throw err;
    const reason =
      err instanceof Error && err.name === 'AbortError' ? 'context deadline exceeded' : String(err);
    throw new StreamElementsError(`${path}: ${reason}`);
  } finally {
    clearTimeout(timer);
  }
}

async function requestJSON<T>(req: KappaRequest, signal: AbortSignal): Promise<T> {
  const res = await fetch(req.url, {
    method: 'GET',
    headers: { Authorization: `Bearer ${req.token}`, Accept: 'application/json' },
    signal
  });
  const text = await readCapped(res, MAX_RESPONSE_BODY_BYTES, req.path);
  if (res.status !== 200)
    throw new StreamElementsError(`${req.path} returned ${res.status}: ${snippet(text)}${authHint(res.status)}`);
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new StreamElementsError(`${req.path}: decoding response: ${(err as Error).message}`);
  }
}

async function readCapped(res: Response, cap: number, path: string): Promise<string> {
  try {
    const reader = res.body?.getReader();
    if (!reader) return await res.text();
    return await decodeCappedChunks(reader, cap, path);
  } catch (err) {
    if (err instanceof StreamElementsError) throw err;
    throw new StreamElementsError(`${path}: reading response: ${String(err)}`);
  }
}

async function decodeCappedChunks(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  cap: number,
  path: string
): Promise<string> {
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
  return joinChunks(chunks, total);
}

function joinChunks(chunks: Uint8Array[], total: number): string {
  const merged = new Uint8Array(total);
  let at = 0;
  for (const c of chunks) {
    merged.set(c, at);
    at += c.byteLength;
  }
  return new TextDecoder().decode(merged);
}

function authHint(status: number): string {
  return status === 401 || status === 403
    ? ' (re-copy the JWT from streamelements.com/dashboard/account/channels, "Show secrets"; tokens expire)'
    : '';
}

const MAX_BODY_SNIPPET = 256;
function snippet(body: string): string {
  let s = body;
  if (s.length > MAX_BODY_SNIPPET) s = s.slice(0, MAX_BODY_SNIPPET) + '…';
  return s.replaceAll('\n', ' ').split(/\s+/).filter(Boolean).join(' ');
}

export function detectStreamElements(raw: Uint8Array | string): boolean {
  const text = decodeText(raw);
  if (text.trim().length === 0) return false;
  let doc: unknown;
  try {
    doc = JSON.parse(text);
  } catch {
    return false;
  }
  const env = envelopeCollections(doc);
  if (!env) return false;
  if (env.commands.some(looksLikeCommand)) return true;
  return env.timers.some(looksLikeTimer);
}

function envelopeCollections(doc: unknown): { commands: unknown[]; timers: unknown[] } | null {
  if (doc === null || typeof doc !== 'object') return null;
  const source = doc as Record<string, unknown>;
  const commands = arrayField(source.commands);
  const timers = arrayField(source.timers);
  if (commands === null || timers === null) return null;
  return { commands, timers };
}

function arrayField(value: unknown): unknown[] | null {
  if (value === undefined) return [];
  return Array.isArray(value) ? value : null;
}

function looksLikeCommand(entry: unknown): boolean {
  if (entry === null || typeof entry !== 'object') return false;
  const c = entry as Record<string, unknown>;
  return typeof c.command === 'string' && typeof c.reply === 'string' && typeof c.accessLevel === 'number';
}

function looksLikeTimer(entry: unknown): boolean {
  if (entry === null || typeof entry !== 'object') return false;
  const t = entry as Record<string, unknown>;
  return t.chatLines !== undefined || t.online !== undefined || t.offline !== undefined;
}

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
  enabled: boolean | undefined;
  enabledOnline: boolean | undefined;
  enabledOffline: boolean | undefined;
}

function decodeBotCommand(entry: unknown): BotCommand {
  if (entry === null || typeof entry !== 'object')
    throw new TypeError('command entry must be an object');
  const e = entry as Record<string, unknown>;
  const cooldown = (e.cooldown ?? {}) as Record<string, unknown>;
  return {
    command: asString(e.command),
    regex: asString(e.regex),
    reply: asString(e.reply),
    aliases: Array.isArray(e.aliases) ? e.aliases.filter((a): a is string => typeof a === 'string') : [],
    keywords: Array.isArray(e.keywords) ? e.keywords.filter((k): k is string => typeof k === 'string') : [],
    cooldownUser: finiteNumber(cooldown.user),
    cooldownGlobal: finiteNumber(cooldown.global),
    type: asString(e.type),
    accessLevel: finiteNumber(e.accessLevel),
    cost: finiteNumber(e.cost),
    enabled: optionalFlag(e.enabled),
    enabledOnline: optionalFlag(e.enabledOnline),
    enabledOffline: optionalFlag(e.enabledOffline)
  };
}

function asString(v: unknown): string {
  return typeof v === 'string' ? v : '';
}

function finiteNumber(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : 0;
}

function optionalFlag(v: unknown): boolean | undefined {
  return typeof v === 'boolean' ? v : undefined;
}

function flexText(v: unknown): string {
  if (v === undefined || v === null) return '';
  if (typeof v === 'string') return v;
  if (Array.isArray(v)) return v.map(textElement).join('\n');
  throw new TypeError('timer message must be a string or an array of strings/{text} objects');
}

function textElement(el: unknown): string {
  if (typeof el === 'string') return el;
  if (!isTextObject(el)) throw new TypeError('timer message array elements must be strings or {text} objects');
  return (el as Record<string, unknown>).text as string;
}

function isTextObject(el: unknown): boolean {
  if (el === null || typeof el !== 'object') return false;
  return typeof (el as Record<string, unknown>).text === 'string';
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
  return readBotTimer(entry as Record<string, unknown>);
}

function readBotTimer(e: Record<string, unknown>): BotTimer {
  const online = timerWindowOf(e.online);
  const offline = timerWindowOf(e.offline);
  return {
    name: asString(e.name),
    text: timerText(e),
    enabled: optionalFlag(e.enabled),
    onlineEnabled: online.enabled,
    onlineInterval: online.interval,
    offlineEnabled: offline.enabled,
    offlineInterval: offline.interval
  };
}

function timerWindowOf(v: unknown): { enabled: boolean | undefined; interval: number } {
  if (v === null || typeof v !== 'object') return { enabled: undefined, interval: 0 };
  const w = v as Record<string, unknown>;
  return {
    enabled: optionalFlag(w.enabled),
    interval: finiteNumber(w.interval)
  };
}

function timerText(e: Record<string, unknown>): string {
  if (e.messages === undefined) return flexText(e.message);
  if (!Array.isArray(e.messages)) throw new TypeError('timer message must be a string or an array of strings/{text} objects');
  return e.messages.map((m) => flexText(m)).join('\n');
}

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

function levelLabel(level: number): string {
  return ACCESS_LEVELS.find((r) => r.level === level)?.label ?? `unknown level ${level}`;
}

const q = (s: string): string => JSON.stringify(s);

export function parseStreamElements(raw: Uint8Array | string): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const env = decodeSeEnvelope(raw);

  const state: SeParseState = { manifest: {}, diags: [], fetchDefs: new Map() };

  state.manifest.commands = [];
  parseCommands((env.commands as unknown[] | undefined) ?? [], state);
  omitIfEmpty(state.manifest, 'commands');
  if (state.fetchDefs.size > 0) state.manifest.fetches = [...state.fetchDefs.values()];

  state.manifest.timers = [];
  parseTimers((env.timers as unknown[] | undefined) ?? [], state);
  omitIfEmpty(state.manifest, 'timers');

  return { manifest: state.manifest, diagnostics: state.diags };
}

function omitIfEmpty(m: ImportManifest, key: 'commands' | 'timers'): void {
  if ((m[key]?.length ?? 0) === 0) delete m[key];
}

function decodeSeEnvelope(raw: Uint8Array | string): Record<string, unknown> {
  const doc = parseEnvelopeJson(decodeText(raw));
  if (!isPlainObject(doc)) throw notEnvelope('JSON must be an object');
  const env = doc as Record<string, unknown>;
  arrayFieldOrThrow(env, 'commands');
  arrayFieldOrThrow(env, 'timers');
  return env;
}

function isPlainObject(doc: unknown): boolean {
  if (doc === null || typeof doc !== 'object') return false;
  return !Array.isArray(doc);
}

function arrayFieldOrThrow(env: Record<string, unknown>, field: string): unknown[] {
  const value = env[field];
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw notEnvelope(`${field} must be an array`);
  return value;
}

function decodeText(raw: Uint8Array | string): string {
  return typeof raw === 'string' ? raw : new TextDecoder().decode(raw);
}

function parseEnvelopeJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch (err) {
    throw new Error(`streamelements: payload is not a commands/timers envelope: ${(err as Error).message}`);
  }
}

function notEnvelope(reason: string): Error {
  return new Error(`streamelements: payload is not a commands/timers envelope: ${reason}`);
}

interface NoteSink {
  idx: number;
  diags: ImportDiagnostic[];
  notes: string[];
  fetch?: FetchSlotSink;
}

function addNote(sink: NoteSink, code: string, message: string): void {
  sink.diags.push(warnDiag(sink.idx, code, message));
  sink.notes.push(message);
}

interface SeParseState {
  manifest: ImportManifest;
  diags: ImportDiagnostic[];
  fetchDefs: Map<string, ManifestFetch>;
}

function parseCommands(entries: unknown[], state: SeParseState): void {
  const commands = state.manifest.commands!;
  for (const entry of entries) {
    const c = decodeCommandOrSkip(entry, state.diags);
    if (!c) continue;
    const problem = commandExclusion(c);
    if (problem) {
      state.diags.push(problem);
      continue;
    }
    appendCommand(c, commands, state);
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

function appendCommand(c: BotCommand, commands: ManifestCommand[], state: SeParseState): void {
  const name = normalizeName(c.command);
  const sink: NoteSink = {
    idx: commands.length,
    diags: state.diags,
    notes: [],
    fetch: makeFetchSlotSink(fetchDefSlug('se', name), state.fetchDefs, state.diags)
  };
  const online = flag(c.enabledOnline);

  const { text, perm } = lossyNotes(c, name, online, sink);
  const { lines, diags: respDiags } = canonicalizeResponse(text, sink.idx);

  const draft: CommandDraft = { c, name, online, perm, lines };
  commands.push(assembleCommand(draft, sink));
  state.diags.push(...respDiags);
  emptyResponseErrors(draft, sink);
  keywordsToTriggers(draft, state);
}

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

  const { text, warns } = translateVariables(c.reply, sink.fetch);
  for (const tok of warns) {
    addNote(sink, CODE.variableUnmapped, `response uses ${tok}, ${unmappedClause(tok)}`);
  }
  return { text, perm };
}

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

function accessLevelNote(c: BotCommand, name: string, sink: NoteSink): ReturnType<typeof mapAccessLevel> {
  const mapped = mapAccessLevel(c.accessLevel);
  if (!mapped.recognized) {
    addNote(sink, CODE.permissionUnmapped,
      `command ${q(name)} requires accessLevel ${c.accessLevel} (${levelLabel(c.accessLevel)}), which has no equivalent here; widened to everyone`);
  }
  return mapped;
}

interface CommandDraft {
  c: BotCommand;
  name: string;
  online: boolean;
  perm: AccessTier;
  lines: string[];
}

function assembleCommand(draft: CommandDraft, sink: NoteSink): ManifestCommand {
  const { c, name, online, perm, lines } = draft;
  const cmd: ManifestCommand = { name, responses: lines };
  const sourceLines = canonicalizeResponse(c.reply, sink.idx).lines;
  if (sourceLines.length > 0) cmd.source_responses = sourceLines;
  const aliases = collectAliases(c, name, sink);
  if (aliases.length > 0) cmd.aliases = aliases;
  cmd.permission = perm;
  if (clampCooldown(c.cooldownGlobal) > 0) cmd.cooldown_seconds = clampCooldown(c.cooldownGlobal);
  if (online && !flag(c.enabledOffline)) cmd.online_only = true;
  if (sink.notes.length > 0) cmd.warnings = sink.notes;
  return cmd;
}

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

function emptyResponseErrors(draft: CommandDraft, sink: NoteSink): void {
  if (draft.lines.length > 0) return;
  if (draft.c.reply.trim() !== '') {
    sink.diags.push(errAt(sink.idx, CODE.responseInvalid, `command ${q(draft.name)} has no usable response after translation`));
  } else {
    sink.diags.push(errAt(sink.idx, CODE.responseInvalid, `command ${q(draft.name)} has no response`));
  }
}

interface KeywordExpansion {
  reply: string;
  commandName: string;
}

function keywordsToTriggers(draft: CommandDraft, state: SeParseState): void {
  const expansion: KeywordExpansion = { reply: draft.c.reply, commandName: draft.name };
  for (const kw of draft.c.keywords) {
    appendKeywordTrigger(kw, expansion, state);
  }
  if (state.manifest.triggers?.length === 0) delete state.manifest.triggers;
}

// One line: commit stores triggers as "phrase => response" rows, so a newline would corrupt them.
function appendKeywordTrigger(kw: string, expansion: KeywordExpansion, state: SeParseState): void {
  const phrase = kw.trim();
  if (phrase === '') return;

  const triggers = (state.manifest.triggers ??= []);
  const idx = triggers.length;
  const { text, warns } = translateVariables(expansion.reply);
  for (const tok of warns) {
    state.diags.push(warnDiag(idx, SE_CODE.triggerVariableUnmapped,
      `keyword ${q(phrase)} response uses ${tok}, ${unmappedClause(tok)}`));
  }

  const { lines, diags: respDiags } = canonicalizeResponse(text, idx);
  retitleResponseCodes(respDiags, TRIGGER_RETITLE);
  state.diags.push(...respDiags);

  const response = lines.join(' ');
  if (response === '') {
    state.diags.push(warnDiag(-1, SE_CODE.triggerInvalidSkipped,
      `keyword ${q(kw)} on command ${q(expansion.commandName)} has no usable phrase/response pair; skipped`));
    return;
  }
  triggers.push({ phrase, response });
}

interface RetitleMap {
  truncated: string;
  lineDropped: string;
}

const TRIGGER_RETITLE: RetitleMap = {
  truncated: SE_CODE.triggerResponseTruncated,
  lineDropped: SE_CODE.triggerResponseLineDropped
};

const TIMER_RETITLE: RetitleMap = {
  truncated: SE_CODE.timerMessageTruncated,
  lineDropped: SE_CODE.timerLineDropped
};

function retitleResponseCodes(diags: ImportDiagnostic[], map: RetitleMap): void {
  for (const d of diags) {
    if (d.code === CODE.responseTruncated) d.code = map.truncated;
    else if (d.code === CODE.responseLineDropped) d.code = map.lineDropped;
  }
}

function parseTimers(entries: unknown[], state: SeParseState): void {
  const timers = state.manifest.timers!;
  for (const entry of entries) {
    const t = decodeTimerOrSkip(entry, state.diags);
    if (!t) continue;
    const label = timerLabel(t);
    const problem = timerExclusion(t, label);
    if (problem) {
      state.diags.push(problem);
      continue;
    }
    appendTimer(t, label, timers, state.diags);
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
  const window = timerWindow(t);
  if (window.widened) {
    diags.push(warnDiag(idx, SE_CODE.timerOfflineOnlyWidened,
      `timer ${q(label)} runs only while offline upstream; timers here fire only while live, so it will run while live instead (widening)`));
  }

  const { text, warns } = translateVariables(t.text);
  for (const tok of warns) {
    diags.push(warnDiag(idx, SE_CODE.timerVariableUnmapped,
      `timer message uses ${tok}, ${unmappedClause(tok)}`));
  }

  const { lines, diags: respDiags } = canonicalizeResponse(text, idx);
  retitleResponseCodes(respDiags, TIMER_RETITLE);
  diags.push(...respDiags);

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

function timerWindow(t: BotTimer): { seconds: number; onlineOnly: boolean; widened: boolean } {
  const clampNegative = (s: number): number => (s < 0 ? 0 : s);
  if (flag(t.onlineEnabled)) {
    return { seconds: clampNegative(t.onlineInterval * 60), onlineOnly: true, widened: false };
  }
  return { seconds: clampNegative(t.offlineInterval * 60), onlineOnly: false, widened: true };
}

function flag(p: boolean | undefined): boolean {
  return p === undefined || p;
}

function firstLine(s: string): string {
  for (const line of s.split('\n')) {
    const t = line.trim();
    if (t !== '') return t;
  }
  return '';
}

const MAX_PASSES = 3;

const LEGACY_HEADS = new Set([
  'user', 'sender', 'source', 'touser', 'target', 'channel',
  'getcount', 'count', 'choose', 'random', 'args'
]);

export function translateVariables(inText: string, fetchSink?: FetchSlotSink): { text: string; warns: string[] } {
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
      const { repl, warned } = classifyToken(tok, fetchSink);
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

function findNext(s: string, from: number): { start: number; end: number } | null {
  let i = from;
  while (i < s.length) {
    const end = delimitedEnd(s, i);
    if (end !== undefined) {
      if (!hasNestedExplicit({ s, from: i + 2, until: end - 1 })) return { start: i, end };
      i += 2;
      continue;
    }
    if (malformedDelimited(s, i)) {
      i += 2;
      continue;
    }
    const braceEnd = legacyBraceEnd(s, i);
    if (braceEnd !== -1) return { start: i, end: braceEnd };
    i++;
  }
  return null;
}

function isDollar(charCode: number): boolean {
  return charCode === 0x24;
}

function opensDelimited(s: string, i: number): boolean {
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  return isDollar(s.charCodeAt(i)) && i + 1 < s.length && (s[i + 1] === '(' || s[i + 1] === '{');
}

function delimitedEnd(s: string, i: number): number | undefined {
  if (!opensDelimited(s, i)) return undefined;
  const end = matchDelimited(s, i);
  return end === -1 ? undefined : end;
}

function malformedDelimited(s: string, i: number): boolean {
  return opensDelimited(s, i) && delimitedEnd(s, i) === undefined;
}

function legacyBraceEnd(s: string, i: number): number {
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  if (s[i] !== '{') return -1;
  const end = matchBrace(s, i);
  if (end !== -1 && legacyCandidate(s.slice(i + 1, end - 1))) return end;
  return -1;
}

interface TokenWindow {
  s: string;
  from: number;
  until: number;
}

function hasNestedExplicit(w: TokenWindow): boolean {
  for (let j = w.from; j < w.until - 1; j++) {
    if (opensFamily(w.s, j) !== '') return true;
  }
  return false;
}

function matchDelimited(s: string, start: number): number {
  const openParen = s[start + 1] === '(';
  const depth = { paren: 0, brace: 0 };
  for (let j = start; j < s.length; j++) {
    const family = opensFamily(s, j);
    if (family !== '') {
      depth[family]++;
      j++;
      continue;
    }
    if (closesDelimited(s[j], openParen, depth)) return j + 1;
  }
  return -1;
}

function opensFamily(s: string, i: number): 'paren' | 'brace' | '' {
  if (!isDollar(s.charCodeAt(i)) || i + 1 >= s.length) return '';
  if (s[i + 1] === '(') return 'paren';
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  if (s[i + 1] === '{') return 'brace';
  return '';
}

function closesDelimited(ch: string, openParen: boolean, depth: ScanDepth): boolean {
  applyDepthStep(ch, depth);
  return reachedOwnClose(ch, openParen, depth);
}

interface ScanDepth {
  paren: number;
  brace: number;
}

function applyDepthStep(ch: string, depth: ScanDepth): void {
  if (ch === '(') {
    depth.paren++;
    return;
  }
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  if (ch === '{') {
    depth.brace++;
    return;
  }
  if (ch === ')') {
    depth.paren--;
    return;
  }
  if (ch === '}') depth.brace--;
}

function reachedOwnClose(ch: string, openParen: boolean, depth: ScanDepth): boolean {
  if (openParen) return ch === ')' && depth.paren === 0 && depth.brace <= 0;
  return ch === '}' && depth.brace === 0 && depth.paren <= 0;
}

function matchBrace(s: string, start: number): number {
  let depth = 0;
  for (let j = start; j < s.length; j++) {
    depth += braceStep(s[j]);
    if (depth === 0) return j + 1;
  }
  return -1;
}

function braceStep(ch: string): number {
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  if (ch === '{') return 1;
  return ch === '}' ? -1 : 0;
}

function legacyCandidate(inner: string): boolean {
  const [head] = splitHead(inner.trim());
  return LEGACY_HEADS.has(head);
}

function isIdentChar(c: string): boolean {
  if (c >= 'A' && c <= 'Z') return true;
  if (c >= 'a' && c <= 'z') return true;
  if (c >= '0' && c <= '9') return true;
  return c === '_';
}

function splitHead(body: string): [head: string, rest: string] {
  let i = 0;
  while (i < body.length && isIdentChar(body[i])) {
    i++;
  }
  return [asciiLower(body.slice(0, i)), body.slice(i)];
}

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

interface TokenView {
  tok: string;
  head: string;
  restRaw: string;
  rest: string;
  delimited: boolean;
}

type TokenOutcome = { repl: string; warned: boolean };

const MAX_FETCH_URL_BYTES = 512;

const urlEncoder = new TextEncoder();

export interface FetchSlotSink {
  acquire(url: string, jsonPath: string[] | undefined): string | null;
}

export const FETCH_DEF_CAP = IMPORT_ITEM_CAPS.commands;

function makeFetchSlotSink(
  baseSlug: string,
  defs: Map<string, ManifestFetch>,
  diags: ImportDiagnostic[]
): FetchSlotSink {
  const byArgs = new Map<string, string>();
  let slots = 0;
  return {
    acquire(url, jsonPath) {
      const argsKey = jsonPath?.length ? `${url} ${jsonPath.join('.')}` : url;
      const existing = byArgs.get(argsKey);
      if (existing !== undefined) return existing;

      const key = slots > 0 ? `${baseSlug}_${slots + 1}` : baseSlug;
      if (!registerFetchDef(defs, fetchDefFor(key, url, jsonPath), diags)) return null;
      slots++;
      byArgs.set(argsKey, key);
      return key;
    }
  };
}

function fetchDefFor(key: string, url: string, jsonPath: string[] | undefined): ManifestFetch {
  return jsonPath?.length
    ? { name: key, url, json_path: jsonPath, source: 'streamelements' }
    : { name: key, url, source: 'streamelements' };
}

function registerFetchDef(
  defs: Map<string, ManifestFetch>,
  def: ManifestFetch,
  diags: ImportDiagnostic[]
): boolean {
  if (defs.has(def.name)) {
    diags.push(warnDiag(-1, 'fetch_def_collision',
      `fetch definition ${q(def.name)} was already synthesized with different contents; the earlier one wins`));
    return false;
  }
  if (defs.size >= FETCH_DEF_CAP) return false;
  defs.set(def.name, def);
  diags.push(warnDiag(-1, 'fetch_def_created', createdFetchDefMessage(def)));
  return true;
}

function parseUrlfetchArgs(body: string): { url: string; jsonPath: string[] | undefined } | null {
  const words = body.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return null;
  const url = words[0];
  if (!/^https?:\/\//i.test(url)) return null;
  if (urlEncoder.encode(url).length > MAX_FETCH_URL_BYTES) return null;
  if (words.length === 1) return { url, jsonPath: undefined };
  const segments = words.slice(1).join('.').split('.').map((w) => w.trim()).filter((w) => w !== '');
  return { url, jsonPath: segments.length > 0 ? segments : undefined };
}

function urlfetchRule(v: TokenView, fetchSink?: FetchSlotSink): TokenOutcome {
  if (!fetchSink || !v.delimited) return unmapped(v);
  const args = parseUrlfetchArgs(v.restRaw);
  if (!args) return unmapped(v);
  const key = fetchSink.acquire(args.url, args.jsonPath);
  if (key === null) return unmapped(v);
  const span = emit('urlfetch', key);
  return span === null ? unmapped(v) : ok(span);
}

function classifyToken(tok: string, fetchSink?: FetchSlotSink): TokenOutcome {
  const v = readToken(tok);
  if (!v) return { repl: tok, warned: false };
  return (TOKEN_RULES[v.head] ?? unmapped)(v, fetchSink);
}

function readToken(tok: string): TokenView | null {
  if (isDelimitedToken(tok)) return tokenOf({ tok, inner: tok.slice(2, -1), delimited: true });
  if (isBareBraceToken(tok)) return tokenOf({ tok, inner: tok.slice(1, -1), delimited: false });
  return null;
}

interface RawToken {
  tok: string;
  inner: string;
  delimited: boolean;
}

function isDelimitedToken(tok: string): boolean {
  return (
    (tok.startsWith('$(') && tok.endsWith(')') && tok.length > 3) ||
    (tok.startsWith('${') && tok.endsWith('}') && tok.length > 3)
  );
}

function isBareBraceToken(tok: string): boolean {
  // brace-literal-ok: inbound scan of the SOURCE product's own $(/${/{ delimiter syntax, never a mint of ours
  return tok.startsWith('{') && tok.endsWith('}') && tok.length > 2;
}

function tokenOf(raw: RawToken): TokenView {
  const [head, restRaw] = splitHead(raw.inner.trim());
  return { tok: raw.tok, head, restRaw, rest: asciiLower(restRaw), delimited: raw.delimited };
}

const ok = (repl: string): TokenOutcome => ({ repl, warned: false });
const silentLiteral = (v: TokenView): TokenOutcome => ({ repl: v.tok, warned: false });
const flaggedLiteral = (v: TokenView): TokenOutcome => ({ repl: v.tok, warned: true });

function unmapped(v: TokenView): TokenOutcome {
  return { repl: v.tok, warned: true };
}

interface BakedIdentitySpec {
  repl: string;
  suffixes: string[];
}

function bakedIdentity(spec: BakedIdentitySpec): (v: TokenView) => TokenOutcome {
  return (v) => (v.rest === '' || spec.suffixes.includes(v.rest) ? ok(spec.repl) : unmapped(v));
}

function headless(v: TokenView): TokenOutcome {
  const range = /^:(\d+)$/.exec(v.restRaw);
  if (range) {
    const m = Number(range[1]);
    return m < 2 ? unmapped(v) : okOrUnmapped(slice(undefined, m - 1), v);
  }
  return v.delimited && v.rest !== '' ? flaggedLiteral(v) : silentLiteral(v);
}

function touserParam(v: TokenView): TokenOutcome {
  return v.restRaw === '' ? ok(emit('touser')!) : unmapped(v);
}

function okOrUnmapped(span: string | null, v: TokenView): TokenOutcome {
  return span === null ? unmapped(v) : ok(span);
}

function positionalRule(v: TokenView): TokenOutcome {
  const n = Number(v.head);
  if (v.restRaw === '') return okOrUnmapped(positional(n), v);
  if (v.restRaw === ':') return okOrUnmapped(n === 1 ? emit('args') : slice(n), v);
  const range = /^:(\d+)$/.exec(v.restRaw);
  if (range) return okOrUnmapped(slice(n, Number(range[1])), v);
  const fallback = /^\|(.+)$/.exec(v.restRaw);
  if (fallback) return okOrUnmapped(positional(n, fallback[1]), v);
  return unmapped(v);
}

function getCounterParam(v: TokenView): TokenOutcome {
  const name = firstWord(v.restRaw);
  if (name === '') return unmapped(v);
  const span = emit('counter', normalizeName(name));
  return span === null ? unmapped(v) : ok(span);
}

function chooseParam(v: TokenView): TokenOutcome {
  if (v.delimited) return unmapped(v);
  const items = pickItems(v.restRaw);
  const span = items ? emit('choice', items.join(',')) : null;
  return span === null ? flaggedLiteral(v) : ok(span);
}

function userParam(v: TokenView): TokenOutcome {
  if (v.rest === '' || v.rest === '.name') return ok(emit('user')!);
  if (v.rest === '.points') return ok(emit('points')!);
  return unmapped(v);
}

function isChannelIdentityRest(rest: string): boolean {
  return rest === '' || rest === '.alias' || rest === '.display_name';
}

function channelParam(v: TokenView): TokenOutcome {
  if (isChannelIdentityRest(v.rest)) return ok(emit('channel')!);
  if (v.rest === '.followers') return ok(emit('followers')!);
  if (v.rest === '.subs') return ok(emit('subs')!);
  return unmapped(v);
}

function timeParam(v: TokenView): TokenOutcome {
  const place = v.restRaw.trim();
  return okOrUnmapped(place === '' ? emit('time') : emit('time', place), v);
}

function mathParam(v: TokenView): TokenOutcome {
  const expr = v.restRaw.trim();
  return expr === '' ? unmapped(v) : okOrUnmapped(emit('math', expr), v);
}

const REPEAT_ARGS = /^\s+(\d+)\s+(.+)$/s;

function repeatParam(v: TokenView): TokenOutcome {
  const m = REPEAT_ARGS.exec(v.restRaw);
  return m ? okOrUnmapped(emit('repeat', `${m[1]}:${m[2]}`), v) : unmapped(v);
}

const TOKEN_RULES: Record<string, (v: TokenView, fetchSink?: FetchSlotSink) => TokenOutcome> = {
  urlfetch: urlfetchRule,
  '': headless,
  user: userParam,
  sender: bakedIdentity({ repl: emit('user')!, suffixes: ['.name'] }),
  source: bakedIdentity({ repl: emit('user')!, suffixes: ['.name'] }),
  touser: touserParam,
  target: bakedIdentity({ repl: emit('touser')!, suffixes: ['.user', '.name'] }),
  channel: channelParam,
  args: (v) => (v.delimited ? unmapped(v) : v.rest === '' ? ok(emit('args')!) : unmapped(v)),
  getcount: getCounterParam,
  count: flaggedLiteral,
  random: classifyRandom,
  choose: chooseParam,
  pointsname: (v) => (v.restRaw === '' ? ok(emit('points.name')!) : unmapped(v)),
  time: timeParam,
  math: mathParam,
  repeat: repeatParam
};

for (let n = 1; n <= POSITIONAL_MAX; n++) TOKEN_RULES[String(n)] = positionalRule;

type RandomRow = (v: TokenView) => string | null;

const RANDOM_ROWS: RandomRow[] = [
  (v) => (v.restRaw === '' ? emit('random') : null),
  dotRandomRange,
  dotRandomNumber,
  dotRandomPick,
  spaceRandomPick,
  plainRandomRange
];

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
  if (v.delimited || v.rest.startsWith('.')) return null;
  const r = parseRange(v.restRaw);
  return r ? rangeKey(r[0], r[1]) : null;
}

function pickKey(items: string[] | null): string | null {
  return items ? emit('choice', items.join(',')) : null;
}

function pickItems(spec: string): string[] | null {
  const raw = splitPickList(spec);
  if (raw === null) return null;
  return sanitizePickItems(raw);
}

interface PickScan {
  raw: string[];
  cur: string;
  quote: string;
}

function splitPickList(spec: string): string[] | null {
  const scan: PickScan = { raw: [], cur: '', quote: '\0' };
  for (const c of spec.trim()) scanPickChar(scan, c);
  if (scan.quote !== '\0') return null;
  flushPick(scan);
  return scan.raw;
}

function scanPickChar(scan: PickScan, c: string): void {
  if (scan.quote !== '\0') {
    absorbQuotedChar(scan, c);
    return;
  }
  if (isQuoteMark(c)) {
    scan.quote = c;
    return;
  }
  if (isPickSeparator(c)) {
    flushPick(scan);
    return;
  }
  scan.cur += c;
}

function absorbQuotedChar(scan: PickScan, c: string): void {
  if (c === scan.quote) {
    scan.quote = '\0';
    return;
  }
  scan.cur += c;
}

function isPickSeparator(c: string): boolean {
  return c === ' ' || c === '\t' || c === ',';
}

function isQuoteMark(c: string): boolean {
  return c === "'" || c === '"' || c === '`';
}

function flushPick(scan: PickScan): void {
  const t = scan.cur.trim();
  if (t !== '') scan.raw.push(t);
  scan.cur = '';
}

function sanitizePickItems(raw: string[]): string[] | null {
  const items: string[] = [];
  for (const r of raw) {
    const t = unwrapQuotes(r.trim());
    if (t === '') continue;
    if (t.includes(',')) return null;
    items.push(t);
  }
  return items.length > 0 ? items : null;
}

function unwrapQuotes(t: string): string {
  const quoted = t.length >= 2 && isQuoteMark(t[0]) && t[t.length - 1] === t[0];
  return quoted ? t.slice(1, -1).trim() : t;
}

function rangeKey(x: number, y: number): string | null {
  return emit('random', `${x}-${y}`);
}

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

function goAtoi(s: string): number | null {
  if (!/^[+-]?\d+$/.test(s)) return null;
  const n = Number(s);
  return Number.isSafeInteger(n) ? n : null;
}

function firstWord(s: string): string {
  const f = s.split(/\s+/).filter(Boolean);
  return f.length > 0 ? f[0] : '';
}
