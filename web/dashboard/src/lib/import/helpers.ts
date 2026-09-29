// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { normalizeName } from '@bagel/kit/importer/validate';
import type {
  CollisionRef,
  CommitResponse,
  ImportDiagnostic,
  ImportFailedItem,
  ImportFailedKind,
  ImportManifest,
  ImportStats,
  PreviewResponse
} from '@bagel/kit';

export type RowKind = 'commands' | 'timers' | 'triggers' | 'quotes';
export type Selection = Record<string, boolean>;

export const ROW_KINDS: readonly RowKind[] = ['commands', 'timers', 'triggers', 'quotes'];

const CODE_PREFIX: Record<RowKind, string> = {
  commands: 'command',
  timers: 'timer',
  triggers: 'trigger',
  quotes: 'quote'
};

export const SKIPPED_CAP = 8;

const rowsOf = (m: ImportManifest, kind: RowKind): unknown[] => (m[kind] as unknown[] | undefined) ?? [];

export function selectionKey(kind: RowKind, i: number): string {
  return `${kind}:${i}`;
}

export function itemDiags(
  diagnostics: ImportDiagnostic[] | undefined,
  kind: RowKind,
  i: number
): ImportDiagnostic[] {
  return (diagnostics ?? []).filter((d) => d.item_index === i && d.code.startsWith(CODE_PREFIX[kind]));
}

const isFatal = (diags: ImportDiagnostic[]) => diags.some((d) => d.severity === 'error');

export function buildSelection(preview: PreviewResponse | null, want: boolean): Selection {
  const m = preview?.manifest;
  const out: Selection = {};
  if (!m) return out;
  for (const kind of ROW_KINDS) {
    rowsOf(m, kind).forEach((_, i) => {
      out[selectionKey(kind, i)] = want && !isFatal(itemDiags(preview.diagnostics, kind, i));
    });
  }
  return out;
}

export function isChecked(selected: Selection, kind: RowKind, i: number): boolean {
  return selected[selectionKey(kind, i)] !== false;
}

export function countRows(m: ImportManifest | undefined): number {
  if (!m) return 0;
  return ROW_KINDS.reduce((n, kind) => n + rowsOf(m, kind).length, 0);
}

export function countPicked(m: ImportManifest | undefined, selected: Selection): number {
  if (!m) return 0;
  let n = 0;
  for (const kind of ROW_KINDS) rowsOf(m, kind).forEach((_, i) => isChecked(selected, kind, i) && n++);
  return n;
}

export function countFatal(preview: PreviewResponse | null): number {
  const m = preview?.manifest;
  if (!m) return 0;
  let n = 0;
  for (const kind of ROW_KINDS)
    rowsOf(m, kind).forEach((_, i) => isFatal(itemDiags(preview.diagnostics, kind, i)) && n++);
  return n;
}

export function buildSelectedManifest(preview: PreviewResponse | null, selected: Selection): string {
  const m = preview?.manifest;
  if (!m) return '{}';
  const out: Record<string, unknown> = {};
  for (const kind of ROW_KINDS) {
    const picked = rowsOf(m, kind).filter((_, i) => isChecked(selected, kind, i));
    out[kind] = picked.length ? picked : undefined;
  }
  if (m.automod && countPicked(m, selected) > 0) out.automod = m.automod;
  return JSON.stringify(out);
}

export function capSkipped(
  skipped: CollisionRef[] | undefined,
  cap: number = SKIPPED_CAP
): { shown: CollisionRef[]; more: number } {
  const all = skipped ?? [];
  return { shown: all.slice(0, cap), more: Math.max(all.length - cap, 0) };
}

export const FAILED_CAP = 8;

const MAX_LABEL_LENGTH = 80;

const ROW_OF_FAILED: Record<Exclude<ImportFailedKind, 'automod'>, RowKind> = {
  command: 'commands',
  timer: 'timers',
  trigger: 'triggers',
  quote: 'quotes'
};

const LABELERS: Record<RowKind, (row: Record<string, string>) => string> = {
  commands: (row) => normalizeName(row.name ?? ''),
  timers: (row) => (row.message ?? '').trim().slice(0, MAX_LABEL_LENGTH),
  triggers: (row) => (row.phrase ?? '').trim().slice(0, MAX_LABEL_LENGTH),
  quotes: (row) => (row.text ?? '').trim().slice(0, MAX_LABEL_LENGTH)
};

export function rowLabel(kind: RowKind, row: unknown): string {
  return LABELERS[kind](row as Record<string, string>);
}

export function capFailed(
  failed: ImportFailedItem[] | undefined,
  cap: number = FAILED_CAP
): { shown: ImportFailedItem[]; more: number } {
  const all = failed ?? [];
  return { shown: all.slice(0, cap), more: Math.max(all.length - cap, 0) };
}

function failedLabels(failed: ImportFailedItem[], kind: ImportFailedKind): Set<string> {
  return new Set(failed.filter((f) => f.kind === kind).map((f) => f.name));
}

export function retryable(failed: ImportFailedItem[] | undefined): ImportFailedItem[] {
  return (failed ?? []).filter((f) => f.reason !== 'invalid');
}

export function buildRetryManifest(sentManifestJson: string, failedItems: ImportFailedItem[] | undefined): string {
  const failed = retryable(failedItems);
  if (!failed.length) return '{}';
  const sent = JSON.parse(sentManifestJson) as ImportManifest;
  const out: Record<string, unknown> = {};
  for (const [kind, rowKind] of Object.entries(ROW_OF_FAILED) as [keyof typeof ROW_OF_FAILED, RowKind][]) {
    const labels = failedLabels(failed, kind);
    const rows = rowsOf(sent, rowKind).filter((row) => labels.has(rowLabel(rowKind, row)));
    if (rows.length) out[rowKind] = rows;
  }
  if (failed.some((f) => f.kind === 'automod') && sent.automod) out.automod = sent.automod;
  return Object.keys(out).length ? JSON.stringify(out) : '{}';
}

const STAT_KEYS: readonly (keyof ImportStats)[] = ['commands', 'timers', 'triggers', 'quotes'];

export function mergeRetry(prev: CommitResponse, next: CommitResponse): CommitResponse {
  const applied = { ...prev.applied };
  for (const key of STAT_KEYS) applied[key] += next.applied[key];
  return { ...prev, applied, failed: next.failed, diagnostics: next.diagnostics };
}
