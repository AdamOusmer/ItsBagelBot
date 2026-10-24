// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// user_id always comes from the authenticated Session, never from caller-supplied data.
import { randomUUID } from 'node:crypto';
import { SERVER_STRATEGIES } from './strategy';
import { emptyStats, errorDiag, refused, type ImportPreviewRequest, type ParseOutcome } from './outcome';
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
import { rpc, RpcError } from '@bagel/kit/server/nats';
import { rowLabel, type RowKind } from '../../import/helpers';
import { logger } from '@bagel/kit/server/logger';
import type {
  CommitResponse,
  ImportDiagnostic,
  ImportFailedItem,
  ImportFailedKind,
  ImportFailedReason,
  ImportManifest,
  ImportSource,
  ImportStats,
  ManifestCommand,
  ManifestTimer,
  ManifestTrigger,
  PreviewResponse,
  TimerDef
} from '@bagel/kit';
import { DEFAULT_CHAT_WINDOW_MINUTES } from '@bagel/kit/timers';

const COMMIT_COMMAND_BATCH = 25;

const MAX_QUOTE_ADDED_BY_LEN = 64;

const MAX_TIMER_INTERVAL_SECONDS = 7 * 86400;

interface GateField {
  label: string;
  min: number;
  max: number;
}

const MIN_LINES_FIELD: GateField = { label: 'minimum chat lines', min: 0, max: 100 };
const WINDOW_FIELD: GateField = { label: 'chat window (minutes)', min: 1, max: 60 };

export type { ImportPreviewRequest };

export type ImportCommitRequest = {
  source: ImportSource | '';
  manifest: ImportManifest;
  overwrite: boolean;
};

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
  failures: ImportFailedItem[];
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
    failures: [],
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
  return {
    applied: ctx.applied,
    skipped: ctx.collisions,
    ...(ctx.failures.length > 0 && { failed: ctx.failures }),
    diagnostics: ctx.diags
  };
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

interface Failure {
  kind: ImportFailedKind;
  name: string;
  reason: ImportFailedReason;
}

function recordFailure(ctx: CommitContext, failure: Failure, err: unknown): void {
  if (!(err instanceof RpcError)) throw err;
  ctx.failures.push(failure);
}

interface RowRef {
  kind: RowKind;
  idx: number;
}

function failRow(ctx: CommitContext, row: RowRef, reason: ImportFailedReason, err: unknown): void {
  const { kind, idx } = row;
  const failureKind = FAILED_KIND[kind];
  recordFailure(ctx, { kind: failureKind, name: rowLabel(kind, ctx.manifest[kind]![idx]), reason }, err);
  ctx.diags.push(errorDiag(idx, CODE.writeFailed, String(err)));
}

const FAILED_KIND: Record<RowKind, Exclude<ImportFailedKind, 'automod'>> = {
  commands: 'command',
  timers: 'timer',
  triggers: 'trigger',
  quotes: 'quote'
};

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
      isActive: !cmd.disabled,
      streamOnlineOnly: !!cmd.online_only,
      perm: cmd.permission ?? 'everyone',
      cooldown: clampCooldown(cmd.cooldown_seconds ?? 0),
      userCooldown: clampCooldown(cmd.user_cooldown_seconds ?? 0),
      allowedUserId: '',
      bumpCounter: ''
    });
    ctx.applied.commands++;
  } catch (err) {
    failRow(ctx, { kind: 'commands', idx }, 'rejected', err);
  }
}

function failUnavailableModules(ctx: CommitContext): void {
  for (const kind of ['timers', 'triggers'] as const) {
    for (const idx of eligibleIndexes(ctx, kind)) {
      ctx.failures.push({ kind: FAILED_KIND[kind], name: rowLabel(kind, ctx.manifest[kind]![idx]), reason: 'module' });
    }
  }
  if (ctx.manifest.automod) ctx.failures.push({ kind: 'automod', name: 'automod', reason: 'module' });
}

async function commitTimersAndTriggers(ctx: CommitContext): Promise<void> {
  if (!ctx.modules) return failUnavailableModules(ctx);
  await applyRows(ctx, moduleBlob(ctx, 'timers'), TIMERS_LEG);
  await applyRows(ctx, moduleBlob(ctx, 'triggers'), TRIGGERS_LEG);
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
      failRow(ctx, { kind: 'quotes', idx }, 'rejected', err);
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

function clampGate(ctx: CommitContext, field: GateField, value: number, idx: number): number {
  const bounded = Math.min(Math.max(value, field.min), field.max);
  if (bounded !== value) {
    ctx.diags.push(warnDiag(idx, CODE.timerGateClamped, `timer ${field.label} ${value} clamped to ${bounded}`));
  }
  return Math.trunc(bounded);
}

function timerGate(ctx: CommitContext, t: ManifestTimer, idx: number): Pick<TimerDef, 'minChatLines' | 'chatWindowMinutes'> {
  return {
    minChatLines: clampGate(ctx, MIN_LINES_FIELD, t.min_chat_lines ?? 0, idx),
    chatWindowMinutes: clampGate(ctx, WINDOW_FIELD, t.chat_window_minutes || DEFAULT_CHAT_WINDOW_MINUTES, idx)
  };
}

function clampInterval(ctx: CommitContext, requested: number, idx: number): number {
  const interval = Math.min(Math.max(requested, MIN_TIMER_INTERVAL_SECONDS), MAX_TIMER_INTERVAL_SECONDS);
  if (interval !== requested) {
    const floor = interval === MIN_TIMER_INTERVAL_SECONDS ? ' (engine floor)' : '';
    ctx.diags.push(warnDiag(idx, CODE.intervalClamped, `interval ${requested}s clamped to ${interval}s${floor}`));
  }
  return interval;
}

interface RowLeg {
  collection: 'timers' | 'triggers';
  build(ctx: CommitContext, blob: Record<string, unknown>, targets: number[]): { configs: Record<string, unknown>; landed: number[] };
}

async function applyRows(ctx: CommitContext, blob: Record<string, unknown>, leg: RowLeg): Promise<void> {
  const targets = eligibleIndexes(ctx, leg.collection);
  if (targets.length === 0) return;
  const { configs, landed } = leg.build(ctx, blob, targets);
  if (landed.length === 0) return;
  try {
    await patchModule(ctx, { name: leg.collection, configs });
    ctx.applied[leg.collection] += landed.length;
  } catch (err) {
    for (const idx of landed) failRow(ctx, { kind: leg.collection, idx }, 'module', err);
  }
}

const TIMERS_LEG: RowLeg = {
  collection: 'timers',
  build(ctx, blob, targets) {
    const merged = Array.isArray(blob.timers) ? [...(blob.timers as TimerDef[])] : [];
    for (const idx of targets) {
      const t = ctx.manifest.timers![idx];
      merged.push({
        id: randomUUID(),
        message: t.message.trim(),
        intervalSeconds: clampInterval(ctx, t.interval_seconds, idx),
        enabled: true,
        ...timerGate(ctx, t, idx),
        allowOffline: t.online_only === false,
        maxFiresPerStream: 0,
        endsAt: ''
      } satisfies TimerDef);
    }
    return { configs: { timers: merged }, landed: targets };
  }
};

const TRIGGERS_LEG: RowLeg = {
  collection: 'triggers',
  build(ctx, blob, targets) {
    const existingRules = typeof blob.rules === 'string' ? blob.rules : '';
    const lines = existingRules
      .trim()
      .split('\n')
      .filter((l) => l.trim() !== '');
    const landed: number[] = [];
    for (const idx of targets) {
      const tr = ctx.manifest.triggers![idx];
      const problem = triggerLineProblem(tr);
      if (problem) {
        ctx.diags.push(errorDiag(idx, CODE.triggerInvalid, problem));
        ctx.failures.push({ kind: 'trigger', name: rowLabel('triggers', tr), reason: 'invalid' });
        continue;
      }
      lines.push(`${tr.phrase.trim()} => ${tr.response.trim()}`);
      landed.push(idx);
    }
    return { configs: { rules: lines.join('\n') }, landed };
  }
};

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
    recordFailure(ctx, { kind: 'automod', name: 'automod', reason: 'module' }, err);
    ctx.diags.push(errorDiag(-1, CODE.writeFailed, `module automod patch failed: ${String(err)}`));
  }
}

function mergeTermList(existingTerms: string[], imported: string[]): { value: string; diags: ImportDiagnostic[] } {
  const terms = [...existingTerms];
  const seen = new Set(terms.map((t) => t.toLowerCase()));
  let dropped = 0;
  for (const raw of imported) {
    const t = raw.trim();
    const key = t.toLowerCase();
    if (t === '' || seen.has(key)) continue;
    seen.add(key);
    if (terms.length >= MAX_AUTOMOD_TERMS) dropped++;
    else terms.push(t);
  }
  return {
    value: terms.join(','),
    diags:
      dropped > 0
        ? [warnDiag(-1, CODE.automodTermsTooMany, `${dropped} term(s) dropped past the ${MAX_AUTOMOD_TERMS}-term limit`)]
        : []
  };
}

function splitTermList(s: string): string[] {
  if (s.trim() === '') return [];
  return s
    .split(',')
    .map((p) => p.trim())
    .filter((p) => p !== '');
}
