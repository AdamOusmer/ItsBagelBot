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

export function parseSnapshot(raw: string | null, stages: readonly string[]): ImportSnapshot | null {
  if (!raw) return null;
  try {
    const v: unknown = JSON.parse(raw);
    if (!isObject(v)) return null;
    const source = typeof v.source === 'string' && isImportSource(v.source) ? v.source : '';
    const stage = typeof v.stage === 'string' && stages.includes(v.stage) ? v.stage : 'pick';
    return {
      source,
      stage,
      overwrite: v.overwrite === true,
      previewResult: isObject(v.previewResult) ? (v.previewResult as unknown as ImportSnapshot['previewResult']) : null,
      commitResult: isObject(v.commitResult) ? (v.commitResult as unknown as ImportSnapshot['commitResult']) : null,
      selected: isObject(v.selected) ? (v.selected as ImportSnapshot['selected']) : {}
    };
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
