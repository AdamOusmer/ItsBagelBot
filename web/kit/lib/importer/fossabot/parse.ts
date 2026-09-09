// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parse layer of the Fossabot config-import source: envelope rows in,
// canonical ImportManifest out. Envelope failures throw FossabotExportError
// (the wizard renders it as a parse failure); everything inside the envelope
// degrades per row, so one unusable command never costs the broadcaster the
// other hundred.
//
// Only commands exist here. Fossabot's public directory publishes nothing else
// (no timers, no keywords, no cooldowns, and commands hidden from the directory
// are simply absent), which the instructions step states before the fetch runs
// so nobody imports and then wonders where their timers went.
//
// No Go parser ever existed for this source, so unlike moobot/ and
// streamelements.ts there is no port-parity golden to reconcile against; the
// expectations in fossabot.test.ts, pinned by testdata/fossabot-golden.json,
// ARE the contract.

import {
  CODE,
  PERM_TIERS,
  canonicalizeResponse,
  clampCooldown,
  commandNameProblem,
  mapPermission,
  normalizeName,
  warnDiag
} from '../validate';
import type { ImportDiagnostic, ImportManifest, ManifestCommand, ManifestFetch, Perm } from '../types';
import { IMPORT_ITEM_CAPS } from '../types';
import { decodeEnvelope } from './envelope';
import type { FbCommand, FbEnvelope } from './envelope';
import { makeFetchSlotSink } from '../nightbot/fetchdefs';
import { translateVariables } from './variables';

// Codes this parser emits beyond the shared CODE table. The *_skipped family
// marks source rows deliberately left out of the manifest: they own no manifest
// slot, so they are attributed to index -1 and name the offender instead.
// command_disabled is restated from the Moobot parser's table rather than
// spelled differently: both mean "the source had this command switched off".
export const FB_CODE = {
  commandBuiltinSkipped: 'command_builtin_skipped',
  commandDisabled: 'command_disabled',
  commandNameInvalid: 'command_name_invalid',
  commandActionPrefixDropped: 'command_action_prefix_dropped',
  commandsCapped: 'manifest_commands_capped'
} as const;

// ACTION_PREFIXES are the chat-action markers Fossabot responses carry. Twitch
// renders ".me text" / "/me text" as an action (coloured, italic); this bot
// posts responses verbatim, so the marker would be echoed as literal text at
// the head of the line. Dropping it keeps the message and loses the styling,
// which is the smaller loss, and the warn says so.
const ACTION_PREFIXES = ['.me ', '/me '];

const q = (s: string): string => JSON.stringify(s);

const errDiag = (d: Omit<ImportDiagnostic, 'severity'>): ImportDiagnostic => ({ severity: 'error', ...d });

// Row is one source command on its way through the row functions: the raw
// feed row plus the normalized name every diagnostic, alias check and lookup
// addresses it by, computed once. Passing the pair as one value is what keeps
// the row functions from each re-deriving (or being handed the wrong) name.
interface Row {
  src: FbCommand;
  name: string;
}

// State is what every row-level function needs and nothing more: the shared
// diagnostics stream, the import-level definition map, the role id → name
// table, and the reference index. Passing it as one value keeps the row
// functions inside the argument budget and keeps a row from ever being handed
// a diagnostics list that is not the real one.
class State {
  readonly diags: ImportDiagnostic[] = [];
  readonly fetchDefs = new Map<string, ManifestFetch>();
  private readonly roleNames = new Map<string, string>();
  private readonly responses = new Map<string, string>();

  constructor(env: FbEnvelope) {
    for (const role of env.roles) this.roleNames.set(role.id, role.name);
    for (const row of env.commands) indexResponse(this.responses, row);
  }

  // roleName resolves one role id to the label mapPermission reads. An id the
  // roles table does not carry falls back to the id itself, which maps to
  // nothing and therefore surfaces as an unmapped-permission warning rather
  // than as a silently wider command.
  roleName(id: string): string {
    return this.roleNames.get(id) ?? id;
  }

  // reference resolves a $(references …) target to the referenced command's
  // RAW response, by name or by alias, case-insensitively. Only custom
  // commands are indexed: a reference to a Fossabot built-in resolves to
  // nothing here and stays literal with a warning, which is honest, since the
  // built-in itself is not coming over either.
  reference(name: string): string | null {
    return this.responses.get(normalizeName(name)) ?? null;
  }

  skip(code: string, message: string): void {
    this.diags.push(warnDiag(-1, code, message));
  }
}

// indexResponse registers one custom row under its name and every alias.
// First writer wins, so two rows claiming one alias resolve deterministically
// (rows are sorted by name before this runs).
function indexResponse(index: Map<string, string>, row: FbCommand): void {
  if (row.type !== 'custom') return;
  for (const key of [row.name, ...row.aliases]) {
    const norm = normalizeName(key);
    if (norm !== '' && !index.has(norm)) index.set(norm, row.response);
  }
}

// Notes is one item's warning sink: every note reaches the diagnostics stream
// AND the item's own `warnings`, which the review screen renders inline, so the
// two can never drift apart by a forgotten push.
class Notes {
  private readonly kept: string[] = [];

  constructor(
    readonly state: State,
    readonly index: number
  ) {}

  add(code: string, message: string): void {
    this.state.diags.push(warnDiag(this.index, code, message));
    this.kept.push(message);
  }

  list(): string[] {
    return this.kept;
  }
}

// parseFossabot translates one commands feed into a manifest plus diagnostics.
export function parseFossabot(bytes: Uint8Array): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const env = decodeEnvelope(bytes);
  const rows = sortByName(env.commands);
  const state = new State({ roles: env.roles, commands: rows });

  const commands = collect(rows, state);
  const manifest: ImportManifest = {};
  if (commands.length > 0) manifest.commands = commands;
  if (state.fetchDefs.size > 0) manifest.fetches = [...state.fetchDefs.values()];
  return { manifest, diagnostics: state.diags };
}

// sortByName fixes the order rows are indexed and emitted in. Fossabot's feed
// order is its own (creation order, roughly), so sorting by normalized name
// makes the manifest, the diagnostic sequence and the synthesized definition
// slugs a pure function of the CONTENT: two fetches of the same channel
// produce identical bytes, which is what the golden pins.
function sortByName(rows: FbCommand[]): FbCommand[] {
  return [...rows].sort((a, b) => compareRows(a, b));
}

function compareRows(a: FbCommand, b: FbCommand): number {
  const byName = normalizeName(a.name).localeCompare(normalizeName(b.name), 'en');
  return byName !== 0 ? byName : a.id.localeCompare(b.id, 'en');
}

// collect walks the sorted rows through the row parser, dropping the ones that
// own no manifest slot. The index a row sees is the index its item will have IN
// THE MANIFEST, which is what diagnostics address.
function collect(rows: FbCommand[], state: State): ManifestCommand[] {
  const out: ManifestCommand[] = [];
  for (let i = 0; i < rows.length; i++) {
    if (out.length >= IMPORT_ITEM_CAPS.commands) {
      state.skip(
        FB_CODE.commandsCapped,
        `${rows.length - i} command(s) dropped past the ${IMPORT_ITEM_CAPS.commands}-item import limit`
      );
      break;
    }
    const item = parseCommandRow(rows[i], new Notes(state, out.length));
    if (item !== null) out.push(item);
  }
  return out;
}

function parseCommandRow(src: FbCommand, notes: Notes): ManifestCommand | null {
  const row: Row = { src, name: normalizeName(src.name) };
  if (refused(row, notes.state)) return null;
  const cmd: ManifestCommand = {
    name: row.name,
    responses: commandResponses(row, notes),
    permission: commandPermission(row, notes)
  };
  return decorate(cmd, row, notes);
}

// refused reports the rows that own no manifest slot at all, each with the
// diagnostic that names it. A built-in is checked first: it is a Fossabot
// feature (its "response" is a description of what the built-in does, not a
// message), so it is not a broken row, it is one that was never ours.
function refused(row: Row, state: State): boolean {
  if (row.src.type !== 'custom') {
    state.skip(
      FB_CODE.commandBuiltinSkipped,
      `${q(row.src.name)} is a built-in Fossabot command, not one of yours; skipped`
    );
    return true;
  }
  if (commandNameProblem(row.name) !== null) {
    state.diags.push(
      errDiag({
        item_index: -1,
        code: FB_CODE.commandNameInvalid,
        message: `command ${q(row.src.name)} is not a usable command name; skipped`
      })
    );
    return true;
  }
  return disabledRefusal(row, state);
}

// A command switched off both online and offline never runs upstream, so
// importing it would resurrect something the broadcaster deliberately silenced.
function disabledRefusal(row: Row, state: State): boolean {
  if (row.src.enabledOnline || row.src.enabledOffline) return false;
  state.skip(FB_CODE.commandDisabled, `command ${q(row.name)} is switched off upstream and was skipped`);
  return true;
}

// decorate adds the fields that are omitted when they carry nothing, so a
// serialized manifest matches what the other parsers emit for the same content.
function decorate(cmd: ManifestCommand, row: Row, notes: Notes): ManifestCommand {
  const aliases = commandAliases(row, notes);
  if (aliases.length > 0) cmd.aliases = aliases;
  const cooldown = clampCooldown(row.src.cooldownSeconds);
  if (cooldown > 0) cmd.cooldown_seconds = cooldown;
  // Fossabot gates each command on the stream being live or offline. Only the
  // live-only combination has an equivalent here; a command enabled offline
  // only would need an "offline_only" this bot does not have, so it comes over
  // as always-on rather than as never-on.
  if (row.src.enabledOnline && !row.src.enabledOffline) cmd.online_only = true;
  if (notes.list().length > 0) cmd.warnings = notes.list();
  return cmd;
}

// commandAliases normalizes the alternate names Fossabot carries per command.
// Duplicates (of the name or of each other) are dropped silently since nothing
// is lost; an alias that cannot be a command name here is dropped WITH a note,
// because the broadcaster's chat has a spelling that will stop working.
function commandAliases(row: Row, notes: Notes): string[] {
  const out: string[] = [];
  const seen = new Set<string>([row.name]);
  for (const raw of row.src.aliases) {
    const alias = normalizeName(raw);
    if (commandNameProblem(alias) !== null) {
      notes.add(CODE.aliasInvalid, `alias ${q(raw)} of command ${q(row.name)} cannot be a command name here; dropped`);
      continue;
    }
    if (seen.has(alias)) continue;
    seen.add(alias);
    out.push(alias);
  }
  return out;
}

// commandPermission resolves Fossabot's per-command role list onto one tier.
//
// Decision record - most permissive wins: Fossabot grants a command to a SET of
// roles and ANY of them may trigger it, while this bot stores one minimum tier.
// So a command listed for Moderator and Broadcaster is imported as mod: taking
// the highest tier instead would lock moderators out of a command they use
// today, and there is no way to express "mods and the broadcaster but not VIPs"
// here anyway. An empty list is Fossabot's own "everyone".
function commandPermission(row: Row, notes: Notes): Perm {
  if (row.src.roleIds.length === 0) return 'everyone';
  const resolved = resolveRoles(row.src.roleIds, notes.state);
  if (resolved.unmapped.length > 0) {
    notes.add(
      CODE.permissionUnmapped,
      `command ${q(row.name)} is granted to ${resolved.unmapped.map(q).join(', ')}, which ${
        resolved.unmapped.length === 1 ? 'has' : 'have'
      } no equivalent here; imported as ${resolved.perm}`
    );
  }
  return resolved.perm;
}

interface ResolvedRoles {
  perm: Perm;
  unmapped: string[];
}

// resolveRoles maps each listed role and keeps the widest tier that mapped.
// Nothing mapped means everyone: every alternative either narrows a
// broadcaster's intent or invents trust, the same call mapPermission makes for
// an unknown label on its own.
function resolveRoles(ids: string[], state: State): ResolvedRoles {
  const unmapped: string[] = [];
  let best: Perm | null = null;
  for (const id of ids) {
    const label = state.roleName(id);
    const { perm, recognized } = mapPermission(label);
    if (recognized) best = widest(best, perm);
    else unmapped.push(label);
  }
  return { perm: best ?? 'everyone', unmapped };
}

function widest(current: Perm | null, next: Perm): Perm {
  if (current === null) return next;
  return PERM_TIERS.indexOf(next) < PERM_TIERS.indexOf(current) ? next : current;
}

// commandResponses translates one command's response into chat-ready lines,
// inlining references and synthesizing urlfetch definitions on the way. An
// empty result carries an error diagnostic so commit skips the command instead
// of writing a mute one.
function commandResponses(row: Row, notes: Notes): string[] {
  const sink = makeFetchSlotSink('fossabot', row.name, notes.state.fetchDefs, notes.state.diags);
  const lookup = (target: string): string | null => notes.state.reference(target);
  const translated = translateVariables(stripActionPrefix(row, notes), { sink, lookup });
  for (const tok of translated.warns) {
    notes.add(CODE.variableUnmapped, `response uses ${tok}, which has no equivalent; left as literal text`);
  }

  const { lines, diags } = canonicalizeResponse(flattenControls(translated.text), notes.index);
  notes.state.diags.push(...diags);
  if (lines.length === 0) notes.state.diags.push(emptyResponseDiag(row, notes));
  return lines;
}

function emptyResponseDiag(row: Row, notes: Notes): ImportDiagnostic {
  return errDiag({
    item_index: notes.index,
    code: CODE.responseInvalid,
    message:
      row.src.response.trim() === ''
        ? `command ${q(row.name)} has no response`
        : `command ${q(row.name)} has no usable response after translation`
  });
}

// flattenControls turns stray C0 control characters into spaces, leaving line
// breaks for canonicalizeResponse to split on.
//
// Decision record: real Fossabot responses carry embedded tabs (the myth
// directory's !newvid does, mid-sentence, where a paste kept the character),
// and this bot's own command validator refuses a response containing ANY
// control character. Without this the command translates cleanly, shows up in
// the review screen, and is then refused at commit for a byte nobody can see:
// swapping it for a space costs nothing visible in chat and keeps the command.
// No warning is attached on purpose, since nothing a broadcaster would
// recognize was lost.
function flattenControls(text: string): string {
  return text.replace(/[\u0000-\u0009\u000b\u000c\u000e-\u001f\u007f]/g, ' ');
}

// stripActionPrefix removes a leading chat-action marker, keeping the message.
function stripActionPrefix(row: Row, notes: Notes): string {
  const lead = row.src.response.trimStart();
  const prefix = ACTION_PREFIXES.find((p) => lead.toLowerCase().startsWith(p));
  if (prefix === undefined) return row.src.response;
  notes.add(
    FB_CODE.commandActionPrefixDropped,
    `command ${q(row.name)} was posted as a ${prefix.trim()} chat action; the text comes over, the action styling does not`
  );
  return lead.slice(prefix.length);
}
