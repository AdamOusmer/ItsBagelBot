// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Envelope layer of the Fossabot parser: what the bytes handed to parseFossabot
// may look like, and how each accepted shape folds into one FbEnvelope the
// parse layer walks.
//
// The bytes normally come from ./fetch, which staples the two cached-API
// replies into {channel, roles, commands}. The bare commands reply
// ({"roles":[…],"commands":[…]}), a lone {"commands":[…]} and a bare array of
// rows are accepted too: they are what a broadcaster gets by saving the
// network response out of their own browser, and refusing them would buy
// nothing.
//
// Row shape, verified against the live cached API 2026-09-07: every row
// carries exactly aliases, enabled_offline, enabled_online, id, name, response,
// role_ids and type. Nothing else is published, cooldowns included, so the
// optional cooldown readers below fire only if Fossabot ever adds them; they
// are never assumed.

export class FossabotExportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'FossabotExportError';
  }
}

// FbRole is one entry of the channel's role table. Fossabot addresses roles by
// id on each command, so the name lives here alone and the parse layer joins
// the two.
export interface FbRole {
  id: string;
  name: string;
}

// FbCommand is one directory row read into the fields this parser uses.
// `type` is 'custom' for a broadcaster's own command and 'default' for a
// Fossabot built-in (commands, dadjoke, permit …), whose response text is a
// description of the built-in rather than something this bot could post.
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

// looksLikeCommand is the shape probe that keeps this parser from claiming
// another bot's export: a Fossabot row pairs `name` with `response`, where
// Nightbot pairs name with message, StreamElements command with reply and
// Moobot identifier with text.
export function looksLikeCommand(row: FbRow): boolean {
  return typeof row.name === 'string' && typeof row.response === 'string';
}

// A file saved out of a Windows editor can carry a UTF-8 BOM, which JSON.parse
// refuses; the decoder below would otherwise report it as a syntax error the
// broadcaster cannot see in their own file.
const BOM = [0xef, 0xbb, 0xbf];

function stripBom(bytes: Uint8Array): Uint8Array {
  return startsWithBom(bytes) ? bytes.subarray(BOM.length) : bytes;
}

function startsWithBom(bytes: Uint8Array): boolean {
  return bytes.length >= BOM.length && BOM.every((b, i) => bytes[i] === b);
}

function decodeJson(bytes: Uint8Array): unknown {
  try {
    // fatal:false replaces invalid UTF-8 rather than throwing on it; a syntax
    // error still surfaces, as the envelope-level failure it is.
    return JSON.parse(new TextDecoder('utf-8', { fatal: false }).decode(stripBom(bytes)));
  } catch (err) {
    throw new FossabotExportError(
      `importer/fossabot: not a Fossabot commands feed: ${(err as Error)?.message ?? String(err)}`
    );
  }
}

// rowsOf pulls one named collection out of the document, accepting both the
// stapled envelope ({commands:[…]}) and the nesting a saved endpoint response
// carries ({commands:{commands:[…]}}).
function rowsOf(doc: FbRow, key: string): FbRow[] {
  const node = doc[key];
  if (Array.isArray(node)) return node.filter(isObj);
  const nested = isObj(node) ? node[key] : undefined;
  return Array.isArray(nested) ? nested.filter(isObj) : [];
}

// readRole keeps a role only when it can be joined: an id-less entry names
// nothing the command rows point at.
function readRole(row: FbRow): FbRole | null {
  const id = asStr(row.id).trim();
  return id === '' ? null : { id, name: asStr(row.name) };
}

// readCommand lifts one row with tolerant readers. Both enabled flags default
// to true when absent: a row that exists at all is on in Fossabot's own model,
// and inventing "disabled" from a missing field would silently drop commands.
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

// readCooldown takes the LARGEST cooldown the row publishes. The cached feed
// carries none at all today, so this only fires on a shape Fossabot has not
// shipped; when several appear (a per-user and a global one, the split every
// other bot in this importer draws) our model has a single cooldown_seconds
// field, and the widest one is the only choice that cannot make an imported
// command chattier than it was upstream.
const COOLDOWN_KEYS = ['cooldown', 'global_cooldown', 'user_cooldown'] as const;

function readCooldown(row: FbRow): number {
  let seconds = 0;
  for (const key of COOLDOWN_KEYS) seconds = Math.max(seconds, Math.trunc(asNum(row[key]) ?? 0));
  return seconds;
}

// decodeEnvelope normalizes any accepted shape into one FbEnvelope, or throws
// FossabotExportError when the bytes are not a Fossabot commands feed at all.
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

// detectFossabot answers whether these bytes are a Fossabot commands feed
// without throwing.
export function detectFossabot(bytes: Uint8Array): boolean {
  try {
    decodeEnvelope(bytes);
    return true;
  } catch {
    return false;
  }
}
