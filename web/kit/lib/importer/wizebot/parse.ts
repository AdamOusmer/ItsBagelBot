// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  CODE,
  PERM_TIERS,
  canonicalizeResponse,
  mapPermission,
  normalizeName,
  warnDiag
} from '../validate';
import type { ImportDiagnostic, ImportManifest, ManifestCommand, Perm } from '../types';
import { decodeEntities } from './entities';
import { decodeEnvelope } from './envelope';
import type { WbRow } from './envelope';
import { translateTags } from './tags';

export const WB_CODE = {
  rowUnparseable: 'command_unparseable_skipped',
  typeUnsupported: 'command_type_unsupported_skipped',
  duplicateSkipped: 'command_duplicate_skipped',
  responseEmpty: 'command_response_empty',
  costDropped: 'command_cost_dropped',
  soundDropped: 'command_sound_dropped',
  randomFlattened: 'command_random_lines_flattened',
  countRemapped: 'command_count_remapped'
} as const;

const KEPT_TYPES = new Set(['SAY', 'SAY_RANDOM']);

const q = (s: string): string => JSON.stringify(s);

const errAt = (code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index: -1,
  code,
  message
});

interface Candidate {
  row: WbRow;
  name: string;
  aliasTokens: string[];
  type: string;
}

class Notes {
  private readonly pending: ImportDiagnostic[] = [];
  private readonly kept: string[] = [];

  add(code: string, message: string): void {
    this.pending.push(warnDiag(-1, code, message));
    this.kept.push(message);
  }

  carry(diags: ImportDiagnostic[]): void {
    this.pending.push(...diags);
  }

  list(): string[] {
    return this.kept;
  }

  flushTo(diags: ImportDiagnostic[], index: number): void {
    for (const d of this.pending) diags.push({ ...d, item_index: index });
  }
}

export function parseWizebot(bytes: Uint8Array): {
  manifest: ImportManifest;
  diagnostics: ImportDiagnostic[];
} {
  const env = decodeEnvelope(bytes);
  const diags: ImportDiagnostic[] = [];
  if (env.malformed > 0) {
    diags.push(
      warnDiag(
        -1,
        WB_CODE.rowUnparseable,
        `skipped ${env.malformed} Wizebot row(s) that carry no readable command columns`
      )
    );
  }

  const commands = collectCommands(pickRows(env.rows, diags), diags);
  const manifest: ImportManifest = {};
  if (commands.length > 0) manifest.commands = commands;
  return { manifest, diagnostics: diags };
}

function pickRows(rows: WbRow[], diags: ImportDiagnostic[]): Candidate[] {
  const named: Candidate[] = [];
  for (const row of rows) {
    const candidate = readCandidate(row, diags);
    if (candidate !== null) named.push(candidate);
  }

  const supported = named.filter((c) => keepType(c, diags));
  supported.sort(byName);
  return dropDuplicates(supported, diags);
}

function readCandidate(row: WbRow, diags: ImportDiagnostic[]): Candidate | null {
  const tokens = decodeEntities(row.aliases)
    .split(/\s+/)
    .filter((t) => t !== '');
  const name = normalizeName(tokens[0] ?? '');
  if (name === '') {
    diags.push(
      errAt(CODE.nameInvalid, `command ${q(row.aliases)} normalizes to an empty name; skipped`)
    );
    return null;
  }
  return { row, name, aliasTokens: tokens.slice(1), type: row.type.trim().toUpperCase() };
}

function keepType(c: Candidate, diags: ImportDiagnostic[]): boolean {
  if (KEPT_TYPES.has(c.type)) return true;
  diags.push(
    warnDiag(
      -1,
      WB_CODE.typeUnsupported,
      `command ${q(`!${c.name}`)} is a ${c.type} command, which posts no chat text; skipped`
    )
  );
  return false;
}

function byName(a: Candidate, b: Candidate): number {
  if (a.name < b.name) return -1;
  return a.name > b.name ? 1 : 0;
}

function dropDuplicates(cands: Candidate[], diags: ImportDiagnostic[]): Candidate[] {
  const seen = new Set<string>();
  const out: Candidate[] = [];
  for (const c of cands) {
    if (seen.has(c.name)) {
      diags.push(
        warnDiag(
          -1,
          WB_CODE.duplicateSkipped,
          `command ${q(`!${c.name}`)} appears more than once upstream (names are case sensitive there, not here); kept the first, skipped the rest`
        )
      );
      continue;
    }
    seen.add(c.name);
    out.push(c);
  }
  return out;
}

function collectCommands(cands: Candidate[], diags: ImportDiagnostic[]): ManifestCommand[] {
  const out: ManifestCommand[] = [];
  for (const c of cands) {
    const notes = new Notes();
    const cmd = buildCommand(c, notes, diags);
    if (cmd === null) continue;
    notes.flushTo(diags, out.length);
    out.push(cmd);
  }
  return out;
}

function buildCommand(
  c: Candidate,
  notes: Notes,
  diags: ImportDiagnostic[]
): ManifestCommand | null {
  const responses = responseLines(c, notes);
  if (responses.length === 0) {
    diags.push(
      warnDiag(
        -1,
        WB_CODE.responseEmpty,
        `command ${q(`!${c.name}`)} publishes no text on the streaming website; skipped`
      )
    );
    return null;
  }

  reportCost(c, notes);
  reportSound(c, notes);

  const cmd: ManifestCommand = { name: c.name, responses, permission: permissionOf(c, notes) };
  const aliases = aliasList(c, notes);
  if (aliases.length > 0) cmd.aliases = aliases;
  if (notes.list().length > 0) cmd.warnings = notes.list();
  const sourceLines = canonicalizeResponse(decodeEntities(c.row.text), -1).lines;
  if (sourceLines.length > 0) cmd.source_responses = sourceLines;
  return cmd;
}

function responseLines(c: Candidate, notes: Notes): string[] {
  const translated = translateTags(decodeEntities(c.row.text));
  for (const tag of translated.warns) {
    notes.add(
      CODE.variableUnmapped,
      `response uses ${tag}, which has no equivalent here; left as literal text`
    );
  }
  if (translated.countRemapped) {
    notes.add(
      WB_CODE.countRemapped,
      'response uses $(cmd_count), imported as {count}: it now counts this command’s own runs from zero, not the running total Wizebot showed'
    );
  }

  const { lines, diags } = canonicalizeResponse(translated.text, -1);
  notes.carry(diags);
  reportRandomLines(c, lines.length, notes);
  return lines;
}

function reportRandomLines(c: Candidate, lineCount: number, notes: Notes): void {
  if (c.type !== 'SAY_RANDOM') return;
  if (lineCount < 2) return;
  notes.add(
    WB_CODE.randomFlattened,
    `command ${q(`!${c.name}`)} picked one of its ${lineCount} lines at random upstream; all of them are posted here`
  );
}

function aliasList(c: Candidate, notes: Notes): string[] {
  const out: string[] = [];
  for (const token of c.aliasTokens) {
    const alias = normalizeName(token);
    if (alias === '') {
      notes.add(CODE.aliasInvalid, `alias ${q(token)} normalizes to an empty name; dropped`);
      continue;
    }
    if (isKnownAlias(alias, c.name, out)) continue;
    out.push(alias);
  }
  return out;
}

function isKnownAlias(alias: string, name: string, taken: string[]): boolean {
  return alias === name || taken.includes(alias);
}

function reportCost(c: Candidate, notes: Notes): void {
  const cost = c.row.cost.trim();
  if (isFreeCost(cost)) return;
  notes.add(
    WB_CODE.costDropped,
    `command ${q(`!${c.name}`)} cost ${costPhrase(cost)} upstream; imported commands are free to run`
  );
}

function isFreeCost(cost: string): boolean {
  return cost === '' || cost === '-';
}

function costPhrase(cost: string): string {
  const amount = cost.slice(1);
  if (cost.startsWith('C')) return `${amount} channel currency`;
  if (cost.startsWith('B')) return `${amount} bits`;
  return q(cost);
}

function reportSound(c: Candidate, notes: Notes): void {
  if (!c.row.sound) return;
  notes.add(
    WB_CODE.soundDropped,
    `command ${q(`!${c.name}`)} also played a sound on stream; only its chat text is imported`
  );
}

const LABEL_SPAN = /<span[^>]*>([\s\S]*?)<\/span>/gi;

const FOLLOWER_QUALIFIER = /\s+\([^)]*\)$/;

function permissionOf(c: Candidate, notes: Notes): Perm {
  const labels = permissionLabels(c.row.permHtml);
  const perms: Perm[] = [];
  for (const label of labels) {
    const { perm, recognized } = mapPermission(label);
    if (!recognized) noteUnmappedTier(c, label, notes);
    perms.push(perm);
  }
  return mostPermissive(perms);
}

function noteUnmappedTier(c: Candidate, label: string, notes: Notes): void {
  notes.add(
    CODE.permissionUnmapped,
    `command ${q(`!${c.name}`)} was limited to ${q(label)} upstream, which has no equivalent here; widened to everyone`
  );
}

function permissionLabels(html: string): string[] {
  const chips = [...html.matchAll(LABEL_SPAN)].map((m) => cleanLabel(m[1]));
  const labels = chips.length > 0 ? chips : [cleanLabel(html)];
  return labels.filter((l) => l !== '');
}

function cleanLabel(raw: string): string {
  let text = raw;
  for (let prev = ''; prev !== text; ) {
    prev = text;
    text = text.replace(/<[^>]*>/g, '');
  }
  return decodeEntities(text).trim().replace(FOLLOWER_QUALIFIER, '').trim();
}

function mostPermissive(perms: Perm[]): Perm {
  let best = PERM_TIERS.length - 1;
  for (const perm of perms) best = Math.min(best, PERM_TIERS.indexOf(perm));
  return perms.length === 0 ? 'everyone' : PERM_TIERS[best];
}
