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

// previewImport translates a source config into a reviewable manifest. A
// failed preview (bad token, undecodable upload) comes back as
// PreviewResponse.error — NOT as a thrown error — so the review step can render
// the reason next to any fatal diagnostics. Only a truly wedged environment
// (NATS down) propagates as a throw, which the action reports as a 502.
export async function previewImport(s: Session, req: ImportPreviewRequest): Promise<PreviewResponse> {
  let manifest: ImportManifest | undefined;
  const parseDiags: ImportDiagnostic[] = [];

  if (req.manifest) {
    // Pre-parsed manifests (browser-side Moobot parse keeps raw uploads off
    // the wire) skip fetch/parse entirely. The caller is untrusted exactly
    // like any other input: validateManifest below still runs, including the
    // per-collection caps.
    manifest = req.manifest;
  } else   if (req.source === 'streamelements') {
    let envelope: string;
    try {
      const fetched = await fetchStreamElements(req.credential ?? '');
      envelope = JSON.stringify({ commands: fetched.commands ?? [], timers: fetched.timers ?? [] });
    } catch (err) {
      return {
        stats: emptyStats(),
        diagnostics: [errorDiag(-1, CODE.fetchFailed, (err as Error).message)],
        error: (err as Error).message
      };
    }
    const parsed = parseStreamElements(envelope);
    manifest = parsed.manifest;
    parseDiags.push(...parsed.diagnostics);
  } else if (req.source === 'streamlabs_desktop') {
    if (!req.file_b64)
      return {
        stats: emptyStats(),
        diagnostics: [errorDiag(-1, CODE.fileRequired, 'upload your Chatbot.db file')],
        error: 'upload your Chatbot.db file'
      };
    const file = Buffer.from(req.file_b64, 'base64');
    if (file.byteLength > MAX_DECODED_FILE_BYTES) {
      const msg = `file is ${file.byteLength} bytes; the limit is ${MAX_DECODED_FILE_BYTES}`;
      return { stats: emptyStats(), diagnostics: [errorDiag(-1, CODE.fileTooLarge, msg)], error: msg };
    }
    try {
      const parsed = await parseStreamLabsDesktop(file);
      manifest = parsed.manifest;
      parseDiags.push(...parsed.diagnostics);
    } catch (err) {
      return {
        stats: emptyStats(),
        diagnostics: [errorDiag(-1, CODE.parseFailed, (err as Error).message)],
        error: (err as Error).message
      };
    }
  } else {
    const msg =
      req.source === ''
        ? 'source required'
        : `${req.source}: unsupported source`;
    return { stats: emptyStats(), diagnostics: [errorDiag(-1, CODE.unsupportedSource, msg)], error: msg };
  }

  return finishPreview(s, manifest!, parseDiags);
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

// commitImport applies a (client-filtered) manifest through the owning
// services' existing write paths.
//
// Decision record — audit trail (2026-08-23): the standalone service wrote an
// import_audits row and returned its id; commit now emits ONE structured pino
// line (user_id, source, applied counts, skipped names, diagnostic count)
// instead. A DB table just for import history was never read by anything but
// humans; logs ship to New Relic with the same retention the rest of the
// dashboard's audit-ish trail gets. Consequence: CommitResponse.audit_id is no
// longer set, so the done screen drops the "recorded as #N" line.
export async function commitImport(s: Session, req: ImportCommitRequest): Promise<CommitResponse> {
  const uid = s.user_id;
  const manifest = req.manifest ?? {};

  const diags = validateManifest(manifest);
  const failed = new FailedItems(diags);

  let existingNames: string[] | null = null;
  try {
    existingNames = await commandNames(uid);
  } catch (err) {
    diags.push({ severity: 'warn', item_index: -1, code: CODE.collisionLookupFailed, message: String(err) });
  }
  const collisions = !req.overwrite && existingNames ? findCollisions(existingNames, manifest) : [];
  const skipCommands = new Set(collisions.filter((c) => c.kind === 'command').map((c) => c.name));
  const skipCounters = new Set(collisions.filter((c) => c.kind === 'counter').map((c) => c.name));

  const applied = emptyStats();

  // --- commands: sequential chunks of COMMIT_COMMAND_BATCH upserts ---------
  const targets = (manifest.commands ?? [])
    .map((cmd, idx) => ({ cmd, idx }))
    .filter(({ cmd, idx }) => !failed.has('commands', idx) && !skipCommands.has(normalizeName(cmd.name)));
  for (let start = 0; start < targets.length; start += COMMIT_COMMAND_BATCH) {
    const chunk = targets.slice(start, start + COMMIT_COMMAND_BATCH);
    for (const { cmd, idx } of chunk) {
      try {
        await upsertCommand(uid, {
          name: normalizeName(cmd.name),
          aliases: (cmd.aliases ?? []).map(normalizeName),
          response: (cmd.responses ?? []).join('\n'),
          isActive: true,
          streamOnlineOnly: !!cmd.online_only,
          perm: cmd.permission ?? 'everyone',
          cooldown: clampCooldown(cmd.cooldown_seconds ?? 0),
          allowedUserId: ''
        });
        applied.commands++;
      } catch (err) {
        diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
      }
    }
  }

  // --- timers/triggers/automod: merge into module blobs --------------------
  // A read failure blocks exactly those three collections — they still count
  // as attempted (partial/failed audit semantics) while quotes and counters
  // stay alive.
  let modules: Awaited<ReturnType<typeof listModules>> | null = null;
  try {
    modules = await listModules(uid);
  } catch (err) {
    diags.push(
      errorDiag(-1, CODE.moduleReadFailed, `modules unavailable (${String(err)}); timers, triggers and automod terms not imported`)
    );
  }
  const blobOf = (name: string): Record<string, unknown> =>
    (modules?.find((m) => m.name === name)?.configs as Record<string, unknown> | undefined) ?? {};

  const timerTargets = eligibleIndexes(failed, 'timers', manifest.timers?.length ?? 0);
  if (timerTargets.length > 0 && modules) {
    applyTimers(uid, blobOf('timers'), manifest, timerTargets, diags, applied);
  }

  const triggerTargets = eligibleIndexes(failed, 'triggers', manifest.triggers?.length ?? 0);
  if (triggerTargets.length > 0 && modules) {
    await applyTriggers(uid, blobOf('triggers'), manifest, triggerTargets, diags, applied);
  }

  // --- quotes ---------------------------------------------------------------
  for (const idx of eligibleIndexes(failed, 'quotes', manifest.quotes?.length ?? 0)) {
    const q = manifest.quotes![idx];
    try {
      await addQuote(uid, {
        text: q.text.trim(),
        addedBy: (q.added_by ?? '').slice(0, MAX_QUOTE_ADDED_BY_LEN),
        createdAt: q.created_at ?? ''
      });
      applied.quotes++;
    } catch (err) {
      diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
    }
  }

  // --- automod terms ---------------------------------------------------------
  if (manifest.automod && modules) {
    await applyAutomodTerms(uid, blobOf('automod'), manifest.automod, diags);
  }

  // --- counters ----------------------------------------------------------------
  for (const idx of eligibleIndexes(failed, 'counters', manifest.counters?.length ?? 0)) {
    const c = manifest.counters![idx];
    const name = normalizeName(c.name);
    if (!req.overwrite && skipCounters.has(name)) continue;
    try {
      await createCounter(uid, name, 'channel');
      await setCounter(uid, name, c.value);
      applied.counters++;
    } catch (err) {
      diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
    }
  }

  // Cache drop so the dashboard reflects the import without waiting out a TTL
  // (commands upserts and modules blob patches both project into cached lists).
  invalidate(`commands:${uid}`, `modules:${uid}`);

  // The audit row's replacement: one structured line per commit.
  logger.info(
    {
      event: 'config_import_commit',
      user_id: uid,
      source: req.source || 'unknown',
      overwrite: !!req.overwrite,
      applied,
      skipped: collisions.map((c) => `${c.kind}:${c.name}`),
      diagnostics: diags.length
    },
    'config import committed'
  );

  return { applied, skipped: collisions, diagnostics: diags };
}

// eligibleIndexes returns the valid indexes of one collection that carry no
// error-severity diagnostic — the shared filter every collection's loop walks
// so validation-doomed items never reach a write path.
function eligibleIndexes(failed: FailedItems, collection: string, length: number): number[] {
  const out: number[] = [];
  for (let i = 0; i < length; i++) {
    if (!failed.has(collection, i)) out.push(i);
  }
  return out;
}

// applyTimers merges the imported timers into the channel's existing "timers"
// blob client-side, then patches the whole "timers" key: modules patch merges
// top-level keys wholesale, so appending server-side is impossible and the
// pre-read is what makes this non-destructive. One patch lands all timers, so
// they count together.
async function applyTimers(
  uid: string,
  blob: Record<string, unknown>,
  manifest: ImportManifest,
  targets: number[],
  diags: ImportDiagnostic[],
  applied: ImportStats
): Promise<void> {
  const existing = Array.isArray(blob.timers) ? (blob.timers as TimerDef[]) : [];
  const merged = [...existing];
  for (const idx of targets) {
    const t = manifest.timers![idx];
    let interval = t.interval_seconds;
    if (interval < MIN_TIMER_INTERVAL_SECONDS) {
      interval = MIN_TIMER_INTERVAL_SECONDS;
      diags.push(
        warnDiag(idx, CODE.intervalClamped, `interval ${t.interval_seconds}s clamped to ${MIN_TIMER_INTERVAL_SECONDS}s (engine floor)`)
      );
    }
    if (interval > MAX_TIMER_INTERVAL_SECONDS) {
      interval = MAX_TIMER_INTERVAL_SECONDS;
      diags.push(warnDiag(idx, CODE.intervalClamped, `interval ${t.interval_seconds}s clamped to ${MAX_TIMER_INTERVAL_SECONDS}s`));
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
    await rpc(`${SUB.modules}.patch`, { user_id: uid, name: 'timers', is_enabled: true, configs: { timers: merged } });
    applied.timers += targets.length;
  } catch (err) {
    diags.push(errorDiag(-1, CODE.writeFailed, `module timers patch failed: ${String(err)}`));
  }
}

// applyTriggers appends "phrase => response" lines to the existing rules
// textarea (sesame parses "[mode:] phrase => response" one rule per line;
// plain lines take the default word-match mode). Items that cannot be
// expressed single-line are dropped here with error diagnostics rather than
// written corrupt.
async function applyTriggers(
  uid: string,
  blob: Record<string, unknown>,
  manifest: ImportManifest,
  targets: number[],
  diags: ImportDiagnostic[],
  applied: ImportStats
): Promise<void> {
  const existingRules = typeof blob.rules === 'string' ? blob.rules : '';
  const lines = existingRules
    .trim()
    .split('\n')
    .filter((l) => l.trim() !== '');
  let landed = 0;
  for (const idx of targets) {
    const tr = manifest.triggers![idx];
    const phrase = tr.phrase.trim();
    const response = tr.response.trim();
    if (phrase.includes('\n') || response.includes('\n')) {
      diags.push(errorDiag(idx, CODE.triggerInvalid, 'phrase and response must be single-line'));
      continue;
    }
    if (phrase.startsWith('#')) {
      diags.push(errorDiag(idx, CODE.triggerInvalid, "phrase must not start with '#' (comment marker)"));
      continue;
    }
    if (phrase.includes('=>')) {
      diags.push(errorDiag(idx, CODE.triggerInvalid, 'phrase must not contain "=>" (rule separator)'));
      continue;
    }
    lines.push(`${phrase} => ${response}`);
    landed++;
  }
  if (landed === 0) return;
  try {
    await rpc(`${SUB.modules}.patch`, { user_id: uid, name: 'triggers', is_enabled: true, configs: { rules: lines.join('\n') } });
    applied.triggers += landed;
  } catch (err) {
    diags.push(errorDiag(-1, CODE.writeFailed, `module triggers patch failed: ${String(err)}`));
  }
}

// applyAutomodTerms merges the imported term lists into the automod module's
// comma-joined textarea values (app/sesame/automod wireConfig). Only the two
// term keys are patched, so the module's level/per-reply toggles survive. The
// automod module has no bucket in ImportStats: its outcome shows through
// diagnostics alone — silence means merged.
async function applyAutomodTerms(
  uid: string,
  blob: Record<string, unknown>,
  terms: NonNullable<ImportManifest['automod']>,
  diags: ImportDiagnostic[]
): Promise<void> {
  const partial: Record<string, string> = {};
  for (const [key, imported] of [
    ['block_terms', terms.block ?? []],
    ['allow_terms', terms.allow ?? []]
  ] as const) {
    const merged = mergeTermList(typeof blob[key] === 'string' ? (blob[key] as string) : '', imported);
    partial[key] = merged.value;
    diags.push(...merged.diags);
  }
  try {
    await rpc(`${SUB.modules}.patch`, { user_id: uid, name: 'automod', is_enabled: true, configs: partial });
  } catch (err) {
    diags.push(errorDiag(-1, CODE.writeFailed, `module automod patch failed: ${String(err)}`));
  }
}

// mergeTermList unions an existing comma-joined list with imported terms,
// deduplicating case-insensitively and capping at MAX_AUTOMOD_TERMS entries
// (2 x 200 x ~100 bytes stays far under the modules service's 16KiB blob cap,
// so hitting the cap mid-commit is impossible rather than handled).
function mergeTermList(existing: string, imported: string[]): { value: string; diags: ImportDiagnostic[] } {
  const seen = new Map<string, true>();
  const out: string[] = [];
  for (const t of splitTermList(existing)) {
    seen.set(t.toLowerCase(), true);
    out.push(t);
  }
  let added = 0;
  let truncated = false;
  for (const raw of imported) {
    const t = raw.trim();
    if (t === '') continue;
    if (seen.has(t.toLowerCase())) continue;
    if (out.length >= MAX_AUTOMOD_TERMS) {
      truncated = true;
      break;
    }
    seen.set(t.toLowerCase(), true);
    out.push(t);
    added++;
  }
  const diags: ImportDiagnostic[] = [];
  if (truncated) {
    diags.push(
      warnDiag(-1, CODE.automodTermsTooMany, `${added} term(s) dropped past the ${MAX_AUTOMOD_TERMS}-term limit`)
    );
  }
  return { value: out.join(','), diags };
}

function splitTermList(s: string): string[] {
  if (s.trim() === '') return [];
  return s
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '');
}
