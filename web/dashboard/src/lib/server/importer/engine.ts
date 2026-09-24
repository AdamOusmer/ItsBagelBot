// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// user_id always comes from the authenticated Session, never from caller-supplied data.
// Import cycle via ./strategy and ./sources/*: read cross-module bindings only inside functions.
import { randomUUID } from 'node:crypto';
import { SERVER_STRATEGIES } from './strategy';
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
} from '@bagel/kit/importer/validate';
import type { Session } from '../session';
import { invalidate, SUB } from '../services';
import { listCommands, listModules, upsertCommand } from '../commands-store';
import { addQuote } from '../quotes-store';
import { rpc } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
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
} from '@bagel/kit';

const COMMIT_COMMAND_BATCH = 25;

const MAX_QUOTE_ADDED_BY_LEN = 64;

const MAX_TIMER_INTERVAL_SECONDS = 7 * 86400;

export type ImportPreviewRequest = {
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

export function emptyStats(): ImportStats {
  return { commands: 0, timers: 0, triggers: 0, quotes: 0 };
}

export function errorDiag(item_index: number, code: string, message: string): ImportDiagnostic {
  return { severity: 'error', item_index, code, message };
}

export type ParseOutcome =
  | { manifest: ImportManifest; diags: ImportDiagnostic[] }
  | { refusal: PreviewResponse };

export interface RefusalDiag {
  code: string;
  message: string;
}

export function refused(diag: RefusalDiag): ParseOutcome {
  return {
    refusal: {
      stats: emptyStats(),
      diagnostics: [errorDiag(-1, diag.code, diag.message)],
      error: diag.message
    }
  };
}

export async function previewImport(s: Session, req: ImportPreviewRequest): Promise<PreviewResponse> {
  let outcome: ParseOutcome;
  if (req.manifest) {
    outcome = preParsedManifest(req.manifest);
  } else {
    const leg = SERVER_STRATEGIES[req.source as ImportSource]?.leg;
    outcome = leg ? await leg(req) : unsupportedSource(String(req.source));
  }

  if ('refusal' in outcome) return outcome.refusal;
  return finishPreview(s, outcome.manifest, outcome.diags);
}

function preParsedManifest(manifest: ImportManifest): ParseOutcome {
  return { manifest, diags: [] };
}

function unsupportedSource(source: string): ParseOutcome {
  return refused({
    code: CODE.unsupportedSource,
    message: source === '' ? 'source required' : `${source}: unsupported source`
  });
}

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

interface CommitContext {
  uid: string;
  source: string;
  manifest: ImportManifest;
  overwrite: boolean;
  failed: FailedItems;
  skipCommands: Set<string>;
  collisions: CommitResponse['skipped'];
  diags: ImportDiagnostic[];
  applied: ImportStats;
  modules: Awaited<ReturnType<typeof listModules>> | null;
}

export async function commitImport(s: Session, req: ImportCommitRequest): Promise<CommitResponse> {
  const manifest = req.manifest ?? {};
  const diags = validateManifest(manifest);

  const ctx: CommitContext = {
    uid: s.user_id,
    source: req.source || 'unknown',
    manifest,
    overwrite: !!req.overwrite,
    failed: new FailedItems(diags),
    skipCommands: new Set(),
    collisions: [],
    diags,
    applied: emptyStats(),
    modules: null
  };

  const existingNames = await commandNamesOrWarn(ctx);
  if (existingNames && !ctx.overwrite) {
    ctx.collisions = findCollisions(existingNames, ctx.manifest);
    ctx.skipCommands = collisionNames(ctx.collisions, 'command');
  }

  await commitCommands(ctx);
  ctx.modules = await loadModules(ctx);
  await commitTimersAndTriggers(ctx);
  await commitQuotes(ctx);
  await commitAutomodTerms(ctx);

  invalidate(`commands:${ctx.uid}`, `modules:${ctx.uid}`);
  logCommit(ctx);
  return { applied: ctx.applied, skipped: ctx.collisions, diagnostics: ctx.diags };
}

async function commandNamesOrWarn(ctx: CommitContext): Promise<string[] | null> {
  try {
    return await commandNames(ctx.uid);
  } catch (err) {
    ctx.diags.push({ severity: 'warn', item_index: -1, code: CODE.collisionLookupFailed, message: String(err) });
    return null;
  }
}

type CollisionKind = 'command';

function collisionNames(collisions: CommitResponse['skipped'], kind: CollisionKind): Set<string> {
  return new Set((collisions ?? []).filter((c) => c.kind === kind).map((c) => c.name));
}

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

function moduleBlob(ctx: CommitContext, name: string): Record<string, unknown> {
  return (ctx.modules?.find((m) => m.name === name)?.configs as Record<string, unknown> | undefined) ?? {};
}

interface ModulePatch {
  name: 'timers' | 'triggers' | 'automod';
  configs: Record<string, unknown>;
}

async function patchModule(ctx: CommitContext, patch: ModulePatch): Promise<void> {
  await rpc(`${SUB.modules}.patch`, { user_id: ctx.uid, name: patch.name, is_enabled: true, configs: patch.configs });
}

async function commitCommands(ctx: CommitContext): Promise<void> {
  const targets = (ctx.manifest.commands ?? [])
    .map((cmd, idx) => ({ cmd, idx }))
    .filter(({ cmd, idx }) => !ctx.failed.has('commands', idx) && !ctx.skipCommands.has(normalizeName(cmd.name)));
  for (let start = 0; start < targets.length; start += COMMIT_COMMAND_BATCH) {
    for (const target of targets.slice(start, start + COMMIT_COMMAND_BATCH)) {
      await upsertOneCommand(ctx, target);
    }
  }
}

interface CommandTarget {
  idx: number;
  cmd: ManifestCommand;
}

async function upsertOneCommand(ctx: CommitContext, target: CommandTarget): Promise<void> {
  const { idx, cmd } = target;
  try {
    await upsertCommand(ctx.uid, {
      name: normalizeName(cmd.name),
      aliases: (cmd.aliases ?? []).map(normalizeName),
      response: (cmd.responses ?? []).join('\n'),
      isActive: true,
      streamOnlineOnly: !!cmd.online_only,
      perm: cmd.permission ?? 'everyone',
      cooldown: clampCooldown(cmd.cooldown_seconds ?? 0),
      allowedUserId: '',
      bumpCounter: ''
    });
    ctx.applied.commands++;
  } catch (err) {
    ctx.diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
  }
}

async function commitTimersAndTriggers(ctx: CommitContext): Promise<void> {
  if (!ctx.modules) return;
  await applyTimers(ctx, moduleBlob(ctx, 'timers'));
  await applyTriggers(ctx, moduleBlob(ctx, 'triggers'));
}

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

async function commitAutomodTerms(ctx: CommitContext): Promise<void> {
  if (!ctx.manifest.automod || !ctx.modules) return;
  await applyAutomodTerms(ctx, moduleBlob(ctx, 'automod'));
}

function logCommit(ctx: CommitContext): void {
  logger.info(
    {
      event: 'config_import_commit',
      user_id: ctx.uid,
      source: ctx.source,
      overwrite: ctx.overwrite,
      applied: ctx.applied,
      skipped: (ctx.collisions ?? []).map((c) => `${c.kind}:${c.name}`),
      diagnostics: ctx.diags.length
    },
    'config import committed'
  );
}

type ManifestCollection = 'commands' | 'timers' | 'triggers' | 'quotes';

function eligibleIndexes(ctx: CommitContext, collection: ManifestCollection): number[] {
  const items = ctx.manifest[collection] ?? [];
  const out: number[] = [];
  for (let i = 0; i < items.length; i++) {
    if (!ctx.failed.has(collection, i)) out.push(i);
  }
  return out;
}

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
      enabled: true,
      minChatLines: 0,
      maxFiresPerStream: 0,
      endsAt: ''
    } satisfies TimerDef);
  }
  try {
    await patchModule(ctx, { name: 'timers', configs: { timers: merged } });
    ctx.applied.timers += targets.length;
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module timers patch failed: ${String(err)}`));
  }
}

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
    await patchModule(ctx, { name: 'triggers', configs: { rules: lines.join('\n') } });
    ctx.applied.triggers += landed;
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module triggers patch failed: ${String(err)}`));
  }
}

function triggerLineProblem(tr: ManifestTrigger): string | null {
  const phrase = tr.phrase.trim();
  const response = tr.response.trim();
  if (phrase.includes('\n') || response.includes('\n')) return 'phrase and response must be single-line';
  if (phrase.startsWith('#')) return "phrase must not start with '#' (comment marker)";
  if (phrase.includes('=>')) return 'phrase must not contain "=>" (rule separator)';
  return null;
}

async function applyAutomodTerms(ctx: CommitContext, blob: Record<string, unknown>): Promise<void> {
  const terms = ctx.manifest.automod!;
  const partial: Record<string, string> = {};
  for (const [key, imported] of [
    ['block_terms', terms.block ?? []],
    ['allow_terms', terms.allow ?? []]
  ] as const) {
    const merged = mergeTermList(splitTermList(typeof blob[key] === 'string' ? (blob[key] as string) : ''), imported);
    partial[key] = merged.value;
    ctx.diags.push(...merged.diags);
  }
  try {
    await patchModule(ctx, { name: 'automod', configs: partial });
  } catch (err) {
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module automod patch failed: ${String(err)}`));
  }
}

interface TermBook {
  seen: Map<string, true>;
  terms: string[];
  added: number;
}

function recordTerm(book: TermBook, t: string): void {
  book.seen.set(t.toLowerCase(), true);
  book.terms.push(t);
}

function mergeTermList(existingTerms: string[], imported: string[]): { value: string; diags: ImportDiagnostic[] } {
  const book: TermBook = { seen: new Map(), terms: [], added: 0 };
  for (const t of existingTerms) {
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

function importTerms(book: TermBook, imported: string[]): boolean {
  for (const raw of imported) {
    if (absorbTerm(raw, book)) return true;
  }
  return false;
}

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
