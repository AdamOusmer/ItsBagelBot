// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export class FossabotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'FossabotExportError';
  }
}

export interface FbRole {
  id: string;
  name: string;
}

export interface FbCommand {
  id: string;
  name: string;
  response: string;
  type: string;
  aliases: string[];
  roleIds: string[];
  enabledOnline: boolean;
  enabledOffline: boolean;
  cooldownSeconds: number;
}

export interface FbEnvelope {
  roles: FbRole[];
  commands: FbCommand[];
}

type FbRow = Record<string, unknown>;

const isObj = (v: unknown): v is FbRow =>
  v !== null && typeof v === 'object' && !Array.isArray(v);

const asStr = (v: unknown): string => (typeof v === 'string' ? v : '');

const asNum = (v: unknown): number | undefined =>
  typeof v === 'number' && Number.isFinite(v) ? v : undefined;

export function looksLikeCommand(row: FbRow): boolean {
  return typeof row.name === 'string' && typeof row.response === 'string';
}

const BOM = [0xef, 0xbb, 0xbf];

function stripBom(bytes: Uint8Array): Uint8Array {
  return startsWithBom(bytes) ? bytes.subarray(BOM.length) : bytes;
}

function startsWithBom(bytes: Uint8Array): boolean {
  return bytes.length >= BOM.length && BOM.every((b, i) => bytes[i] === b);
}

function decodeJson(bytes: Uint8Array): unknown {
  try {
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(stripBom(bytes)));
  } catch (err) {
    throw new FossabotExportError(
      `importer/fossabot: not a Fossabot commands feed: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

function rowsOf(doc: FbRow, key: string): FbRow[] {
  const node = doc[key];
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node[key] : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

function readRole(row: FbRow): FbRole | null {
  const id = asStr(row.id).trim();
  return id === '' ? null : { id, name: asStr(row.name) };
}

function readCommand(row: FbRow): FbCommand {
  return {
    id: asStr(row.id),
    name: asStr(row.name),
    response: asStr(row.response),
    type: asStr(row.type).trim().toLowerCase(),
    aliases: strList(row.aliases),
    roleIds: strList(row.role_ids),
    enabledOnline: row.enabled_online !== false,
    enabledOffline: row.enabled_offline !== false,
    cooldownSeconds: readCooldown(row)
  };
}

function strList(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((e): e is string => typeof e === 'string') : [];
}

const COOLDOWN_KEYS = ['cooldown', 'global_cooldown', 'user_cooldown'] as const;

function readCooldown(row: FbRow): number {
  let seconds = 0;
  for (const key of COOLDOWN_KEYS) seconds = Math.max(seconds, Math.trunc(asNum(row[key]) ?? 0));
  return seconds;
}

export function decodeEnvelope(bytes: Uint8Array): FbEnvelope {
  const doc = decodeJson(bytes);
  const rows = Array.isArray(doc) ? doc.filter(isObj) : keyedRows(doc);
  if (!rows.some(looksLikeCommand))
    throw new FossabotExportError(
      'importer/fossabot: no Fossabot commands here (each row needs name and response)'
    );
  return {
    roles: isObj(doc) ? rowsOf(doc, 'roles').map(readRole).filter(isRole) : [],
    commands: rows.map(readCommand)
  };
}

function keyedRows(doc: unknown): FbRow[] {
  if (!isObj(doc))
    throw new FossabotExportError(
      'importer/fossabot: not a Fossabot commands feed: JSON must be an object or an array'
    );
  return rowsOf(doc, 'commands');
}

const isRole = (r: FbRole | null): r is FbRole => r !== null;

export function detectFossabot(bytes: Uint8Array): boolean {
  try {
    decodeEnvelope(bytes);
    return true;
  } catch {
    return false;
  }
}
