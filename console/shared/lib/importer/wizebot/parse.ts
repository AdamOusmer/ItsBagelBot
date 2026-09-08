// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Parse layer of the Wizebot config-import source: command-list rows in,
// canonical ImportManifest out. A list that is not a list at all throws
// WizebotExportError (the wizard renders it as a parse failure); everything
// inside it degrades per row, so one unusable command never costs the
// broadcaster the other fifty.
//
// Wizebot's streaming website publishes commands and nothing else: no timers,
// no quotes, no counters, no spam filters. This parser therefore fills exactly
// one manifest collection, and the instructions copy says as much so the
// broadcaster is not left wondering where their timers went.
//
// Decision record - rows are sorted by normalized name BEFORE they are
// indexed. The upstream list arrives in the table's own display order, which
// is not stable across pages of the same channel; diagnostics address items by
// index, so sorting first is what makes the golden test meaningful (same
// posture as the StreamLabs Chatbot source).
//
// Decision record - what is skipped and why. Wizebot commands come in six
// types, of which SAY and SAY_RANDOM are the only ones that put text in chat.
// SCREEN (a browser-source animation), ONLY_SOUND, ACTION (a /me the public
// list does not spell out) and URL do something this bot's command engine
// cannot reproduce, and importing them as empty or half-translated commands
// would hand the broadcaster a list of commands that answer nothing. They are
// skipped by name instead, so the review step shows what was left behind.

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

// Codes this parser emits beyond the shared CODE table. The *_skipped family
// marks source rows deliberately left out of the manifest: they own no
// manifest slot, so they are attributed to index -1 and name the offender.
export const WB_CODE = {
  rowUnparseable: 'command_unparseable_skipped',
  typeUnsupported: 'command_type_unsupported_skipped',
  duplicateSkipped: 'command_duplicate_skipped',
  responseEmpty: 'command_response_empty',
  costDropped: 'command_cost_dropped',
  soundDropped: 'command_sound_dropped',
  randomFlattened: 'command_random_lines_flattened'
} as const;

// KEPT_TYPES are the row types that carry chat text (see the decision record).
const KEPT_TYPES = new Set(['SAY', 'SAY_RANDOM']);

const q = (s: string): string => JSON.stringify(s);

const errAt = (code: string, message: string): ImportDiagnostic => ({
  severity: 'error',
  item_index: -1,
  code,
  message
});

// Candidate is one row that could become a command: its normalized name, the
// alias tokens behind it and the row it came from.
interface Candidate {
  row: WbRow;
  name: string;
  aliasTokens: string[];
  type: string;
}

// Notes is one item's warning sink. Its findings are held until the item is
// known to be kept, THEN flushed at the index the item actually landed on:
// a row can still drop out after translation (empty response), and a note
// pushed eagerly would address a manifest slot that shifted underneath it.
class Notes {
  private readonly pending: ImportDiagnostic[] = [];
  private readonly kept: string[] = [];

  add(code: string, message: string): void {
    this.pending.push(warnDiag(-1, code, message));
    this.kept.push(message);
  }

  // carry takes canonicalizeResponse's own findings, which are informational
  // rather than inline warnings, so they reach diagnostics but not the item's
  // rendered warning list.
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

// parseWizebot translates one published command list into a manifest plus
// diagnostics.
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

// --- row selection -----------------------------------------------------------

// pickRows turns rows into the ordered, deduplicated candidate list the
// manifest is built from, reporting every row it drops on the way.
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

// readCandidate splits the alias column ("!Yuki  !Yukizuri": whitespace
// separated, first token is the command name) and normalizes the name the way
// the commands service would. Entities are decoded first: names carry them too
// ("!d&eacute;gage").
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

// dropDuplicates keeps the first row of each normalized name. Wizebot lets a
// channel hold both "!Ouf" and "!ouf" (its own lookup is case sensitive);
// storage here is not, so the second one would collide at write time.
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

// --- command building --------------------------------------------------------

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

  // Fields are omitted when empty so a serialized manifest matches what the
  // other parsers emit for the same content.
  const cmd: ManifestCommand = { name: c.name, responses, permission: permissionOf(c, notes) };
  const aliases = aliasList(c, notes);
  if (aliases.length > 0) cmd.aliases = aliases;
  if (notes.list().length > 0) cmd.warnings = notes.list();
  return cmd;
}

// responseLines decodes, translates and canonicalizes one row's text column.
function responseLines(c: Candidate, notes: Notes): string[] {
  const translated = translateTags(decodeEntities(c.row.text));
  for (const tag of translated.warns) {
    notes.add(
      CODE.variableUnmapped,
      `response uses ${tag}, which has no equivalent here; left as literal text`
    );
  }

  const { lines, diags } = canonicalizeResponse(translated.text, -1);
  notes.carry(diags);
  reportRandomLines(c, lines.length, notes);
  return lines;
}

// A SAY_RANDOM command posts ONE of its lines, picked at random. This bot's
// commands post every line they carry, so a multi-line one is flattened: the
// content survives, the randomness does not.
function reportRandomLines(c: Candidate, lineCount: number, notes: Notes): void {
  if (c.type !== 'SAY_RANDOM') return;
  if (lineCount < 2) return;
  notes.add(
    WB_CODE.randomFlattened,
    `command ${q(`!${c.name}`)} picked one of its ${lineCount} lines at random upstream; all of them are posted here`
  );
}

// aliasList normalizes the alias tokens behind the name, dropping the ones
// that cannot become a command name and any that fold onto the name itself.
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

// reportCost names a currency or bits price the import cannot carry: this bot
// runs commands for free, and silently dropping a price the broadcaster set
// deliberately would be a behaviour change they never see.
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

// costPhrase reads Wizebot's price column: "C<n>" is channel currency, "B<n>"
// is bits, anything else is quoted as written rather than guessed at.
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

// --- permissions -------------------------------------------------------------

// LABEL_SPAN matches one permission chip of the published table. The column is
// rendered HTML ('<span class="label label-warning">VIPs</span>'), and several
// chips mean several allowed tiers.
const LABEL_SPAN = /<span[^>]*>([\s\S]*?)<\/span>/gi;

// FOLLOWER_QUALIFIER strips the parenthetical Wizebot appends to the follower
// tier ("Followers (0 J.)" is "followers for 0 days"). The leading whitespace
// is required by the pattern on purpose: "User(s)" carries no space, so it
// survives intact and stays unrecognized, which is exactly right (it is a
// named-user allowlist, not a tier).
const FOLLOWER_QUALIFIER = /\s+\([^)]*\)$/;

// permissionOf resolves the permission column onto one tier.
//
// Decision record - the column is ANY-OF: "Subscribers VIPs Moderators" means
// any of those three may run the command. This bot's commands carry ONE
// minimum tier, so the translation takes the MOST PERMISSIVE mapped tier
// (lowest in PERM_TIERS), which is the only choice that never takes a command
// away from viewers who could run it upstream. The alternative, the most
// restrictive tier, would silently lock subscribers out of a command their
// channel let them run.
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

// permissionLabels lifts each chip's text out of the rendered column. A column
// that carries text but no chip is read as one label, so a markup change
// upstream degrades to "one unrecognized tier" rather than to "everyone".
function permissionLabels(html: string): string[] {
  const chips = [...html.matchAll(LABEL_SPAN)].map((m) => cleanLabel(m[1]));
  const labels = chips.length > 0 ? chips : [cleanLabel(html)];
  return labels.filter((l) => l !== '');
}

function cleanLabel(raw: string): string {
  // Markup is stripped to a fixpoint rather than in one pass: a single pass
  // leaves "<scr<span>ipt>" reading as "<script>", which CodeQL flags
  // (js/incomplete-multi-character-sanitization) even though this text is chat
  // prose that is never rendered as markup. The loop cannot spin, since every
  // pass shortens the string or ends it.
  let text = raw;
  for (let prev = ''; prev !== text; ) {
    prev = text;
    text = text.replace(/<[^>]*>/g, '');
  }
  return decodeEntities(text).trim().replace(FOLLOWER_QUALIFIER, '').trim();
}

// mostPermissive picks the widest audience of the tiers a command allows. No
// chips at all means the command was open to everyone upstream.
function mostPermissive(perms: Perm[]): Perm {
  let best = PERM_TIERS.length - 1;
  for (const perm of perms) best = Math.min(best, PERM_TIERS.indexOf(perm));
  return perms.length === 0 ? 'everyone' : PERM_TIERS[best];
}
