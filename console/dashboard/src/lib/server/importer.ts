// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Local config-import engine: since the standalone importer service was folded
// into the dashboard (2026-08-23), preview and commit run IN THIS PROCESS,
// driving the same owning services' subjects the dashboard already talks to.
// There is no bagel.rpc.importer.* anymore and no importer pod.
//
//   preview: streamelements   → kappa v2 API fetch + parse (shared TS port)
//            moobot           → manifest arrives pre-parsed by the browser
//            streamlabs_desktop → Chatbot.db parsed here (sql.js, see module)
//          …then validateManifest + collision lookup against live commands.
//
//   commit : re-validates the posted manifest, skips items carrying an
//          error-severity diagnostic or a collision (unless overwrite), then
//          drives writes through the existing stores/RPCs: commands-store
//          upsert (chunks of 25, sequential within a chunk — checkpointing,
//          mirroring the old service's fan-out), one merge-patch per touched
//          module blob (timers/triggers/automod), quote.add rows, and loyalty
//          counter create/set pairs.
//
// Identity rule (C3, carried over unchanged): user_id ALWAYS comes from the
// authenticated Session here and never from caller-supplied data.
import { randomUUID } from 'node:crypto';
import { fetchStreamElements, parseStreamElements } from '@bagel/shared/importer/streamelements';
import { parseStreamLabsDesktop } from '@bagel/shared/importer/streamlabs-desktop';
import {
  CODE,
  FailedItems,
  clampCooldown,
  findCollisions,
  normalizeName,
  stats as statsOf,
  validateManifest,
  warnDiag,
  MAX_AUTOMOD_TERMS,
  MIN_TIMER_INTERVAL_SECONDS
} from '@bagel/shared/importer/validate';
import type { Session } from './session';
import { invalidate, SUB } from './services';
import { listCommands, listModules, upsertCommand } from './commands-store';
import { addQuote } from './quotes-store';
import { createCounter, setCounter } from './loyalty-store';
import { rpc } from '@bagel/shared/server/nats';
import { logger } from '@bagel/shared/server/logger';
import type {
  CommitResponse,
  ImportDiagnostic,
  ImportManifest,
  ImportSource,
  ImportStats,
  ManifestCommand,
  ManifestTrigger,
  PreviewResponse,
  TimerDef
} from '@bagel/shared';

// Server-side backstop on uploaded files (was the RPC handler's gate). The
// form action refuses >20MB before encoding; this holds for any future caller
// of this module and bounds what the SQLite parser ever materializes.
const MAX_DECODED_FILE_BYTES = 25 << 20;

// How many command upserts commit sends before moving to the next chunk.
// Within a chunk requests stay sequential: the commands upsert path is
// write-behind already, so parallelizing buys queue contention, not latency;
// the chunking exists so a huge import checkpoints instead of monopolizing
// this request for its whole budget.
const COMMIT_COMMAND_BATCH = 25;

// maxQuoteAddedByLen mirrors the quote schema's added_by column cap.
const MAX_QUOTE_ADDED_BY_LEN = 64;

// Timer interval clamps restate sesame's engine floor (minTimerInterval = 30s):
// below it a timer arms an expire/fire/re-arm loop the engine refuses. There is
// no engine ceiling, but an interval past a week is a source typo, so clamp
// there too rather than write a timer that never fires in a human's lifetime.
const MAX_TIMER_INTERVAL_SECONDS = 7 * 86400;

export type ImportPreviewRequest = {
  // API-backed sources take a credential, file sources take file_b64 (base64
  // of the export), and the browser-parsed Moobot flow takes manifest — the
  // engine then skips fetch/parse and only re-validates + resolves collisions.
  source: ImportSource | '';
  credential?: string;
  file_b64?: string;
  manifest?: ImportManifest;
};

export type ImportCommitRequest = {
  source: ImportSource | '';
  manifest: ImportManifest;
  overwrite: boolean;
};

function emptyStats(): ImportStats {
  return { commands: 0, timers: 0, triggers: 0, quotes: 0, counters: 0 };
}

function errorDiag(item_index: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'error', item_index, code, message };
}

// ParseOutcome is one source's fetch/parse leg: either the translated
// manifest with its diagnostics, or a full refusal response explaining why
// nothing could be previewed.
type ParseOutcome =
  | { manifest: ImportManifest; diags: ImportDiagnostic[] }
  | { refusal: PreviewResponse };

function refused(code: string, message: string): ParseOutcome {
  return {
    refusal: {
      stats: emptyStats(),
      diagnostics: [errorDiag(-1, code, message)],
      error: message
    }
  };
}

// SOURCE_LEGS holds one fetch+parse function per API/file-backed source. The
// browser-parsed Moobot flow posts its manifest directly and has no leg here.
const SOURCE_LEGS: Partial<Record<ImportSource, (req: ImportPreviewRequest) => Promise<ParseOutcome>>> = {
  streamelements: streamelementsLeg,
  streamlabs_desktop: streamlabsDesktopLeg
};

// previewImport translates a source config into a reviewable manifest. A
// failed preview (bad token, undecodable upload) comes back as
// PreviewResponse.error — NOT as a thrown error — so the review step can render
// the reason next to any fatal diagnostics. Only a truly wedged environment
// (NATS down) propagates as a throw, which the action reports as a 502.
export async function previewImport(s: Session, req: ImportPreviewRequest): Promise<PreviewResponse> {
  let outcome: ParseOutcome;
  if (req.manifest) {
    outcome = preParsedManifest(req.manifest);
  } else {
    const leg = SOURCE_LEGS[req.source as ImportSource];
    outcome = leg ? await leg(req) : unsupportedSource(String(req.source));
  }

  if ('refusal' in outcome) return outcome.refusal;
  return finishPreview(s, outcome.manifest, outcome.diags);
}

function preParsedManifest(manifest: ImportManifest): ParseOutcome {
  // Pre-parsed manifests (browser-side Moobot parse keeps raw uploads off
  // the wire) skip fetch/parse entirely. The caller is untrusted exactly
  // like any other input: validateManifest below still runs, including the
  // per-collection caps.
  return { manifest, diags: [] };
}

async function streamelementsLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  let envelope: string;
  try {
    const fetched = await fetchStreamElements(req.credential ?? '');
    envelope = JSON.stringify({ commands: fetched.commands ?? [], timers: fetched.timers ?? [] });
  } catch (err) {
    return refused(CODE.fetchFailed, (err as Error).message);
  }
  const parsed = parseStreamElements(envelope);
  return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
}

async function streamlabsDesktopLeg(req: ImportPreviewRequest): Promise<ParseOutcome> {
  if (!req.file_b64) return refused(CODE.fileRequired, 'upload your Chatbot.db file');

  const file = Buffer.from(req.file_b64, 'base64');
  if (file.byteLength > MAX_DECODED_FILE_BYTES) {
    return refused(
      CODE.fileTooLarge,
      `file is ${file.byteLength} bytes; the limit is ${MAX_DECODED_FILE_BYTES}`
    );
  }

  try {
    const parsed = await parseStreamLabsDesktop(file);
    return { manifest: parsed.manifest, diags: [...parsed.diagnostics] };
  } catch (err) {
    return refused(CODE.parseFailed, (err as Error).message);
  }
}

function unsupportedSource(source: string): ParseOutcome {
  return refused(
    CODE.unsupportedSource,
    source === '' ? 'source required' : `${source}: unsupported source`
  );
}

// finishPreview is the shared tail of every preview flow: canonicalization +
// validation, stats and the collision lookup against the channel's live
// command names.
async function finishPreview(
  s: Session,
  manifest: ImportManifest,
  parseDiags: ImportDiagnostic[]
): Promise<PreviewResponse> {
  const diags = [...parseDiags, ...validateManifest(manifest)];
  const resp: PreviewResponse = {
    manifest,
    diagnostics: diags,
    stats: statsOf(manifest)
  };

  try {
    resp.collisions = findCollisions(await commandNames(s.user_id), manifest);
  } catch (err) {
    // Fail open on collisions (empty list) but say why: a projector blip must
    // not block a preview, while commit re-checks against live state anyway.
    resp.diagnostics = [
      ...(resp.diagnostics ?? []),
      { severity: 'warn', item_index: -1, code: CODE.collisionLookupFailed, message: String(err) }
    ];
  }
  return resp;
}

async function commandNames(userId: string): Promise<string[]> {
  const commands = await listCommands(userId);
  const names: string[] = [];
  for (const c of commands) {
    names.push(c.name);
    names.push(...(c.aliases ?? []));
  }
  return names;
}

// CommitContext groups what every commit leg reads and mutates, replacing the
// six-argument signatures the legs used to carry: identity (uid), the request
// shape (manifest/overwrite), the skip sets from collision lookup, the shared
// diagnostic stream and the applied tally.
interface CommitContext {
  uid: string;
  manifest: ImportManifest;
  overwrite: boolean;
  failed: FailedItems;
  skipCommands: Set<string>;
  skipCounters: Set<string>;
  collisions: CommitResponse['skipped'];
  diags: ImportDiagnostic[];
  applied: ImportStats;
  // modules is null when the read failed; timers/triggers/automod then stay
  // unimported while quotes and counters proceed.
  modules: Awaited<ReturnType<typeof listModules>> | null;
}

// commitImport applies a (client-filtered) manifest through the owning
// services' existing write paths. The legs run in a fixed order — command
// upserts, module-blob timers/triggers, quote rows, automod terms, counter
// create/set — mirroring the old service's fan-out sequence.
//
// Decision record — audit trail (2026-08-23): the standalone service wrote an
// import_audits row and returned its id; commit now emits ONE structured pino
// line (user_id, source, applied counts, skipped names, diagnostic count)
// instead. A DB table just for import history was never read by anything but
// humans; logs ship to New Relic with the same retention the rest of the
// dashboard's audit-ish trail gets. Consequence: CommitResponse.audit_id is no
// longer set, so the done screen drops the "recorded as #N" line.
export async function commitImport(s: Session, req: ImportCommitRequest): Promise<CommitResponse> {
  const manifest = req.manifest ?? {};
  const diags = validateManifest(manifest);
  const failed = new FailedItems(diags);

  const collisions = await resolveCollisions(s.user_id, !!req.overwrite, manifest, diags);
  const ctx: CommitContext = {
    uid: s.user_id,
    manifest,
    overwrite: !!req.overwrite,
    failed,
    skipCommands: collisionNames(collisions, 'command'),
    skipCounters: collisionNames(collisions, 'counter'),
    collisions,
    diags,
    applied: emptyStats(),
    modules: null
  };

  await commitCommands(ctx);
  ctx.modules = await loadModules(ctx);
  await commitTimersAndTriggers(ctx);
  await commitQuotes(ctx);
  await commitAutomodTerms(ctx);
  await commitCounters(ctx);

  // Cache drop so the dashboard reflects the import without waiting out a TTL
  // (commands upserts and modules blob patches both project into cached lists).
  invalidate(`commands:${ctx.uid}`, `modules:${ctx.uid}`);
  logCommit(req.source || 'unknown', ctx);
  return { applied: ctx.applied, skipped: ctx.collisions, diagnostics: ctx.diags };
}

// resolveCollisions looks up the channel's live command names for the skip
// sets. A lookup failure degrades to a warn (fail open): commit re-checks at
// write time anyway, so a projector blip must not block an import.
async function resolveCollisions(
  uid: string,
  overwrite: boolean,
  manifest: ImportManifest,
  diags: ImportDiagnostic[]
): Promise<CommitResponse['skipped']> {
  let existingNames: string[] | null = null;
  try {
    existingNames = await commandNames(uid);
  } catch (err) {
    diags.push({ severity: 'warn', item_index: -1, code: CODE.collisionLookupFailed, message: String(err) });
  }
  return existingNames && !overwrite ? findCollisions(existingNames, manifest) : [];
}

function collisionNames(collisions: CommitResponse['skipped'], kind: string): Set<string> {
  return new Set((collisions ?? []).filter((c) => c.kind === kind).map((c) => c.name));
}

// loadModules reads the channel's module blobs; a read failure blocks exactly
// timers/triggers/automod — those collections still count as attempted
// (partial/failed audit semantics) while quotes and counters stay alive.
async function loadModules(ctx: CommitContext): Promise<Awaited<ReturnType<typeof listModules>> | null> {
  try {
    return await listModules(ctx.uid);
  } catch (err) {
    ctx.diags.push(
      errorDiag(-1, CODE.moduleReadFailed, `modules unavailable (${String(err)}); timers, triggers and automod terms not imported`)
    );
    return null;
  }
}

// moduleBlob exposes one module's stored configs for client-side merging.
function moduleBlob(ctx: CommitContext, name: string): Record<string, unknown> {
  return (ctx.modules?.find((m) => m.name === name)?.configs as Record<string, unknown> | undefined) ?? {};
}

// commitCommands upserts the eligible commands in sequential chunks of
// COMMIT_COMMAND_BATCH: within a chunk requests stay sequential (the commands
// upsert path is write-behind already, so parallelizing buys queue contention,
// not latency), while chunking lets a huge import checkpoint instead of
// monopolizing this request.
async function commitCommands(ctx: CommitContext): Promise<void> {
  const targets = (ctx.manifest.commands ?? [])
    .map((cmd, idx) => ({ cmd, idx }))
    .filter(({ cmd, idx }) => !ctx.failed.has('commands', idx) && !ctx.skipCommands.has(normalizeName(cmd.name)));
  for (let start = 0; start < targets.length; start += COMMIT_COMMAND_BATCH) {
    for (const { cmd, idx } of targets.slice(start, start + COMMIT_COMMAND_BATCH)) {
      await upsertOneCommand(ctx, idx, cmd);
    }
  }
}

async function upsertOneCommand(ctx: CommitContext, idx: number, cmd: ManifestCommand): Promise<void> {
  try {
    await upsertCommand(ctx.uid, {
      name: normalizeName(cmd.name),
      aliases: (cmd.aliases ?? []).map(normalizeName),
      response: (cmd.responses ?? []).join('\n'),
      isActive: true,
      streamOnlineOnly: !!cmd.online_only,
      perm: cmd.permission ?? 'everyone',
      cooldown: clampCooldown(cmd.cooldown_seconds ?? 0),
      allowedUserId: ''
    });
    ctx.applied.commands++;
  } catch (err) {
    ctx.diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
  }
}

// commitTimersAndTriggers merges both collections into their module blobs;
// each applies only when it has eligible targets and the module read survived.
async function commitTimersAndTriggers(ctx: CommitContext): Promise<void> {
  if (!ctx.modules) return;
  await applyTimers(ctx, moduleBlob(ctx, 'timers'));
  await applyTriggers(ctx, moduleBlob(ctx, 'triggers'));
}

// commitQuotes writes quote rows one by one; a failed row is reported and the
// rest proceed.
async function commitQuotes(ctx: CommitContext): Promise<void> {
  for (const idx of eligibleIndexes(ctx, 'quotes')) {
    const q = ctx.manifest.quotes![idx];
    try {
      await addQuote(ctx.uid, {
        text: q.text.trim(),
        addedBy: (q.added_by ?? '').slice(0, MAX_QUOTE_ADDED_BY_LEN),
        createdAt: q.created_at ?? ''
      });
      ctx.applied.quotes++;
    } catch (err) {
      ctx.diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
    }
  }
}

// commitAutomodTerms patches the automod term lists when the manifest carries
// any and the module read survived.
async function commitAutomodTerms(ctx: CommitContext): Promise<void> {
  if (!ctx.manifest.automod || !ctx.modules) return;
  await applyAutomodTerms(ctx, moduleBlob(ctx, 'automod'));
}

// commitCounters creates then sets each eligible counter; colliding names are
// skipped unless the import overwrites.
async function commitCounters(ctx: CommitContext): Promise<void> {
  for (const idx of eligibleIndexes(ctx, 'counters')) {
    const c = ctx.manifest.counters![idx];
    const name = normalizeName(c.name);
    if (!ctx.overwrite && ctx.skipCounters.has(name)) continue;
    try {
      await createCounter(ctx.uid, name, 'channel');
      await setCounter(ctx.uid, name, c.value);
      ctx.applied.counters++;
    } catch (err) {
      ctx.diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
    }
  }
}

// logCommit is the audit row's replacement: one structured line per commit.
function logCommit(source: string, ctx: CommitContext): void {
  logger.info(
    {
      event: 'config_import_commit',
      user_id: ctx.uid,
      source,
      overwrite: ctx.overwrite,
      applied: ctx.applied,
      skipped: (ctx.collisions ?? []).map((c) => `${c.kind}:${c.name}`),
      diagnostics: ctx.diags.length
    },
    'config import committed'
  );
}

// ManifestCollection names one manifest array the commit legs walk.
type ManifestCollection = 'commands' | 'timers' | 'triggers' | 'quotes' | 'counters';

// eligibleIndexes returns the valid indexes of one collection that carry no
// error-severity diagnostic — the shared filter every collection's loop walks
// so validation-doomed items never reach a write path.
function eligibleIndexes(ctx: CommitContext, collection: ManifestCollection): number[] {
  const items = ctx.manifest[collection] ?? [];
  const out: number[] = [];
  for (let i = 0; i < items.length; i++) {
    if (!ctx.failed.has(collection, i)) out.push(i);
  }
  return out;
}

// applyTimers merges the imported timers into the channel's existing "timers"
// blob client-side, then patches the whole "timers" key: modules patch merges
// top-level keys wholesale, so appending server-side is impossible and the
// pre-read is what makes this non-destructive. One patch lands all timers, so
// they count together.
async function applyTimers(ctx: CommitContext, blob: Record<string, unknown>): Promise<void> {
  const targets = eligibleIndexes(ctx, 'timers');
  if (targets.length === 0) return;

  const existing = Array.isArray(blob.timers) ? (blob.timers as TimerDef[]) : [];
  const merged = [...existing];
  for (const idx of targets) {
    const t = ctx.manifest.timers![idx];
    let interval = t.interval_seconds;
    if (interval < MIN_TIMER_INTERVAL_SECONDS) {
      interval = MIN_TIMER_INTERVAL_SECONDS;
      ctx.diags.push(
        warnDiag(idx, CODE.intervalClamped, `interval ${t.interval_seconds}s clamped to ${MIN_TIMER_INTERVAL_SECONDS}s (engine floor)`)
      );
    }
    if (interval > MAX_TIMER_INTERVAL_SECONDS) {
      interval = MAX_TIMER_INTERVAL_SECONDS;
      ctx.diags.push(warnDiag(idx, CODE.intervalClamped, `interval ${t.interval_seconds}s clamped to ${MAX_TIMER_INTERVAL_SECONDS}s`));
    }
    merged.push({
      id: randomUUID(),
      message: t.message.trim(),
      intervalSeconds: interval,
      // Field names mirror sesame's engine-side timer shape (the worker reads
      // this blob directly), not the dashboard's TimerDef casing choices.
      enabled: true
    } satisfies TimerDef);
  }
  try {
    await rpc(`${SUB.modules}.patch`, { user_id: ctx.uid, name: 'timers', is_enabled: true, configs: { timers: merged } });
    ctx.applied.timers += targets.length;
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module timers patch failed: ${String(err)}`));
  }
}

// applyTriggers appends "phrase => response" lines to the existing rules
// textarea (sesame parses "[mode:] phrase => response" one rule per line;
// plain lines take the default word-match mode). Items that cannot be
// expressed single-line are dropped here with error diagnostics rather than
// written corrupt.
async function applyTriggers(ctx: CommitContext, blob: Record<string, unknown>): Promise<void> {
  const targets = eligibleIndexes(ctx, 'triggers');
  if (targets.length === 0) return;

  const existingRules = typeof blob.rules === 'string' ? blob.rules : '';
  const lines = existingRules
    .trim()
    .split('\n')
    .filter((l) => l.trim() !== '');
  let landed = 0;
  for (const idx of targets) {
    const tr = ctx.manifest.triggers![idx];
    const problem = triggerLineProblem(tr);
    if (problem) {
      ctx.diags.push(errorDiag(idx, CODE.triggerInvalid, problem));
      continue;
    }
    lines.push(`${tr.phrase.trim()} => ${tr.response.trim()}`);
    landed++;
  }
  if (landed === 0) return;
  try {
    await rpc(`${SUB.modules}.patch`, { user_id: ctx.uid, name: 'triggers', is_enabled: true, configs: { rules: lines.join('\n') } });
    ctx.applied.triggers += landed;
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module triggers patch failed: ${String(err)}`));
  }
}

// triggerLineProblem names why a trigger cannot ride the rules textarea's
// line grammar, or null when it can. The three refusals mirror sesame's own
// parser: newlines split rules, '#' opens a comment, '=>' is the separator.
function triggerLineProblem(tr: ManifestTrigger): string | null {
  const phrase = tr.phrase.trim();
  const response = tr.response.trim();
  if (phrase.includes('\n') || response.includes('\n')) return 'phrase and response must be single-line';
  if (phrase.startsWith('#')) return "phrase must not start with '#' (comment marker)";
  if (phrase.includes('=>')) return 'phrase must not contain "=>" (rule separator)';
  return null;
}

// applyAutomodTerms merges the imported term lists into the automod module's
// comma-joined textarea values (app/sesame/automod wireConfig). Only the two
// term keys are patched, so the module's level/per-reply toggles survive. The
// automod module has no bucket in ImportStats: its outcome shows through
// diagnostics alone — silence means merged.
async function applyAutomodTerms(ctx: CommitContext, blob: Record<string, unknown>): Promise<void> {
  const terms = ctx.manifest.automod!;
  const partial: Record<string, string> = {};
  for (const [key, imported] of [
    ['block_terms', terms.block ?? []],
    ['allow_terms', terms.allow ?? []]
  ] as const) {
    const merged = mergeTermList(typeof blob[key] === 'string' ? (blob[key] as string) : '', imported);
    partial[key] = merged.value;
    ctx.diags.push(...merged.diags);
  }
  try {
    await rpc(`${SUB.modules}.patch`, { user_id: ctx.uid, name: 'automod', is_enabled: true, configs: partial });
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module automod patch failed: ${String(err)}`));
  }
}

// TermBook accumulates the merged term list while deduplicating
// case-insensitively, counting how many imports actually landed.
interface TermBook {
  seen: Map<string, true>;
  terms: string[];
  added: number;
}

function recordTerm(book: TermBook, t: string): void {
  book.seen.set(t.toLowerCase(), true);
  book.terms.push(t);
}

// mergeTermList unions an existing comma-joined list with imported terms,
// capping at MAX_AUTOMOD_TERMS entries (2 x 200 x ~100 bytes stays far under
// the modules service's 16KiB blob cap, so hitting the cap mid-commit is
// impossible rather than handled).
function mergeTermList(existing: string, imported: string[]): { value: string; diags: ImportDiagnostic[] } {
  const book: TermBook = { seen: new Map(), terms: [], added: 0 };
  for (const t of splitTermList(existing)) {
    recordTerm(book, t);
  }
  const truncated = importTerms(book, imported);
  return {
    value: book.terms.join(','),
    diags: truncated
      ? [warnDiag(-1, CODE.automodTermsTooMany, `${book.added} term(s) dropped past the ${MAX_AUTOMOD_TERMS}-term limit`)]
      : []
  };
}

// importTerms folds the imported list in; returns whether absorption stopped
// early at the term cap.
function importTerms(book: TermBook, imported: string[]): boolean {
  for (const raw of imported) {
    if (absorbTerm(raw, book)) return true;
  }
  return false;
}

// absorbTerm adds one imported term unless it is blank or a duplicate;
// returns whether the cap stopped absorption (everything past it is dropped).
function absorbTerm(raw: string, book: TermBook): boolean {
  const t = raw.trim();
  if (t === '') return false;
  if (book.seen.has(t.toLowerCase())) return false;
  if (book.terms.length >= MAX_AUTOMOD_TERMS) return true;
  recordTerm(book, t);
  book.added++;
  return false;
}

function splitTermList(s: string): string[] {
  if (s.trim() === '') return [];
  return s
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '');
}
