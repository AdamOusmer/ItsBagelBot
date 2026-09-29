// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type {
  CollisionRef,
  ImportDiagnostic,
  ImportManifest,
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
