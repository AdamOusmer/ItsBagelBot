// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export class NightbotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'NightbotExportError';
  }
}

export interface NbEnvelope {
  commands: NbRow[];
  timers: NbRow[];
  blacklist: string[];
}

export type NbRow = Record<string, unknown>;

export const isObj = (v: unknown): v is NbRow =>
  v !== null && typeof v === 'object' && !Array.isArray(v);

export const asStr = (v: unknown): string => (typeof v === 'string' ? v : '');

export const asNum = (v: unknown): number | undefined =>
  typeof v === 'number' && Number.isFinite(v) ? v : undefined;

export function looksLikeCommand(row: NbRow): boolean {
  return typeof row.message === 'string' && typeof row.name === 'string';
}

export function looksLikeTimer(row: NbRow): boolean {
  return typeof row.message === 'string' && row.interval !== undefined;
}

function stripBom(bytes: Uint8Array): Uint8Array {
  return bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf
    ? bytes.subarray(3)
    : bytes;
}

function decodeJson(bytes: Uint8Array): unknown {
  try {
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(stripBom(bytes)));
  } catch (err) {
    throw new NightbotExportError(
      `importer/nightbot: not a JSON export file: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

function rowsOf(doc: NbRow, key: string): NbRow[] {
  const node = doc[key];
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node[key] : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

function blacklistTerms(doc: NbRow): string[] {
  const out: string[] = [];
  const take = (v: unknown): void => {
    if (typeof v === 'string') {
      out.push(...v.split('\n'));
      return;
    }
    if (Array.isArray(v)) for (const t of v) if (typeof t === 'string') out.push(t);
  };
  take(doc.blacklist);
  for (const filter of spamFilters(doc)) take(filter.blacklist);
  return out;
}

function spamFilters(doc: NbRow): NbRow[] {
  const node = doc.spam_protection;
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node.spam_protection : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

function arrayEnvelope(doc: unknown[]): NbEnvelope {
  const commands = doc.filter(isObj);
  if (!commands.some(looksLikeCommand))
    throw new NightbotExportError(
      'importer/nightbot: that array holds no Nightbot commands (each row needs name and message)'
    );
  return { commands, timers: [], blacklist: [] };
}

export function decodeEnvelope(bytes: Uint8Array): NbEnvelope {
  const doc = decodeJson(bytes);
  if (Array.isArray(doc)) return arrayEnvelope(doc);
  if (!isObj(doc))
    throw new NightbotExportError(
      'importer/nightbot: not a Nightbot export: JSON must be an object or an array'
    );

  const env: NbEnvelope = {
    commands: rowsOf(doc, 'commands'),
    timers: rowsOf(doc, 'timers'),
    blacklist: blacklistTerms(doc)
  };
  if (isEmpty(env))
    throw new NightbotExportError(
      'importer/nightbot: no commands, timers or spam-protection terms found in this Nightbot account'
    );
  return env;
}

function isEmpty(env: NbEnvelope): boolean {
  return env.commands.length === 0 && env.timers.length === 0 && env.blacklist.length === 0;
}

export function detectNightbot(bytes: Uint8Array): boolean {
  let env: NbEnvelope;
  try {
    env = decodeEnvelope(bytes);
  } catch {
    return false;
  }
  if (env.commands.some(looksLikeCommand)) return true;
  if (env.timers.some(looksLikeTimer)) return true;
  return env.blacklist.length > 0;
}
