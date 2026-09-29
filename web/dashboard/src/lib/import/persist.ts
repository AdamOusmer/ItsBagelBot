// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { isImportSource } from '@bagel/kit/importer/strategy';
import type { ImportSnapshot } from './session.svelte';

const KEY = 'bb-welcome-import';
const SELECTION_KEY = 'bb-welcome-import-selection';

let retained: ImportSnapshot | null = null;

export function retainedSnapshot(): ImportSnapshot | null {
  return retained;
}

export function retainSnapshot(snap: ImportSnapshot) {
  retained = snap;
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

const objectOr = <T>(v: unknown, fallback: T): T => (isObject(v) ? (v as unknown as T) : fallback);

const sourceOf = (v: unknown): ImportSnapshot['source'] => (typeof v === 'string' && isImportSource(v) ? v : '');

const stageOf = (v: unknown, stages: readonly string[]): string =>
  typeof v === 'string' && stages.includes(v) ? v : 'pick';

function snapshotFrom(v: Record<string, unknown>, stages: readonly string[]): ImportSnapshot {
  return {
    source: sourceOf(v.source),
    stage: stageOf(v.stage, stages),
    overwrite: v.overwrite === true,
    previewResult: objectOr<ImportSnapshot['previewResult']>(v.previewResult, null),
    commitResult: objectOr<ImportSnapshot['commitResult']>(v.commitResult, null),
    selected: objectOr<ImportSnapshot['selected']>(v.selected, {})
  };
}

export function parseSnapshot(raw: string | null, stages: readonly string[]): ImportSnapshot | null {
  if (!raw) return null;
  try {
    const v: unknown = JSON.parse(raw);
    return isObject(v) ? snapshotFrom(v, stages) : null;
  } catch {
    return null;
  }
}

export function loadSnapshot(stages: readonly string[]): ImportSnapshot | null {
  try {
    const base = parseSnapshot(sessionStorage.getItem(KEY), stages);
    if (!base) return null;
    const sel = JSON.parse(sessionStorage.getItem(SELECTION_KEY) ?? 'null') as unknown;
    if (isObject(sel)) {
      base.selected = (sel.selected as ImportSnapshot['selected']) ?? base.selected;
      base.overwrite = sel.overwrite === true;
    }
    return base;
  } catch {
    return null;
  }
}

export function saveSnapshot(snap: ImportSnapshot) {
  retained = snap;
  try {
    sessionStorage.setItem(KEY, JSON.stringify({ ...snap, selected: undefined, overwrite: undefined }));
    saveSelection(snap);
  } catch {
  }
}

export function saveSelection(snap: Pick<ImportSnapshot, 'selected' | 'overwrite'>) {
  try {
    sessionStorage.setItem(SELECTION_KEY, JSON.stringify({ selected: snap.selected, overwrite: snap.overwrite }));
  } catch {
  }
}

export function clearSnapshot() {
  retained = null;
  try {
    sessionStorage.removeItem(KEY);
    sessionStorage.removeItem(SELECTION_KEY);
  } catch {
  }
}
