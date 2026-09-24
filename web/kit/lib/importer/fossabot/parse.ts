// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export const FB_CODE = {
  commandBuiltinSkipped: 'command_builtin_skipped',
  commandDisabled: 'command_disabled',
  commandNameInvalid: 'command_name_invalid',
  commandActionPrefixDropped: 'command_action_prefix_dropped',
  commandsCapped: 'manifest_commands_capped'
} as const;

const ACTION_PREFIXES = ['.me ', '/me '];

const q = (s: string): string => JSON.stringify(s);

const errDiag = (d: Omit<ImportDiagnostic, 'severity'>): ImportDiagnostic => ({ severity: 'error', ...d });

interface Row {
  src: FbCommand;
  name: string;
}

class State {
  readonly diags: ImportDiagnostic[] = [];
  readonly fetchDefs = new Map<string, ManifestFetch>();
  private readonly roleNames = new Map<string, string>();
  private readonly responses = new Map<string, string>();

  constructor(env: FbEnvelope) {
    for (const role of env.roles) this.roleNames.set(role.id, role.name);
    for (const row of env.commands) indexResponse(this.responses, row);
  }

  roleName(id: string): string {
    return this.roleNames.get(id) ?? id;
  }

  reference(name: string): string | null {
    return this.responses.get(normalizeName(name)) ?? null;
  }

  skip(code: string, message: string): void {
    this.diags.push(warnDiag(-1, code, message));
  }
}

function indexResponse(index: Map<string, string>, row: FbCommand): void {
  if (row.type !== 'custom') return;
  for (const key of [row.name, ...row.aliases]) {
    const norm = normalizeName(key);
    if (norm !== '' && !index.has(norm)) index.set(norm, row.response);
  }
}

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

function sortByName(rows: FbCommand[]): FbCommand[] {
  return [...rows].sort((a, b) => compareRows(a, b));
}

function compareRows(a: FbCommand, b: FbCommand): number {
  const byName = normalizeName(a.name).localeCompare(normalizeName(b.name), 'en');
  return byName !== 0 ? byName : a.id.localeCompare(b.id, 'en');
}

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

function disabledRefusal(row: Row, state: State): boolean {
  if (row.src.enabledOnline || row.src.enabledOffline) return false;
  state.skip(FB_CODE.commandDisabled, `command ${q(row.name)} is switched off upstream and was skipped`);
  return true;
}

function decorate(cmd: ManifestCommand, row: Row, notes: Notes): ManifestCommand {
  const aliases = commandAliases(row, notes);
  if (aliases.length > 0) cmd.aliases = aliases;
  const cooldown = clampCooldown(row.src.cooldownSeconds);
  if (cooldown > 0) cmd.cooldown_seconds = cooldown;
  if (row.src.enabledOnline && !row.src.enabledOffline) cmd.online_only = true;
  if (notes.list().length > 0) cmd.warnings = notes.list();
  const sourceLines = canonicalizeResponse(row.src.response, notes.index).lines;
  if (sourceLines.length > 0) cmd.source_responses = sourceLines;
  return cmd;
}

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

function flattenControls(text: string): string {
  return text.replace(/[\u0000-\u0009\u000b\u000c\u000e-\u001f\u007f]/g, ' ');
}

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
