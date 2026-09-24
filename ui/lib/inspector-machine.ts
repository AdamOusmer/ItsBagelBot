// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type InspectorStatus = 'idle' | 'saving' | 'saved' | 'error';

export type InspectorState<T> = {
  selectedId: string | null;
  committed: T | null;
  draft: T | null;
  dirty: boolean;
  status: InspectorStatus;
  submitted?: { resourceId: string; requestId: string; snapshot: T };
};

export type SaveOutcome<T> =
  | { type: 'success'; committed?: T }
  | { type: 'error' };

const clone = <T>(v: T): T => {
  try {
    return structuredClone(v);
  } catch {
    return JSON.parse(JSON.stringify(v)) as T;
  }
};

const same = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b);

export function initial<T>(): InspectorState<T> {
  return { selectedId: null, committed: null, draft: null, dirty: false, status: 'idle' };
}

export function openClean<T>(id: string, committed: T): InspectorState<T> {
  return {
    selectedId: id,
    committed,
    draft: clone(committed),
    dirty: false,
    status: 'idle'
  };
}

export function edit<T>(state: InspectorState<T>, draft: T): InspectorState<T> {
  const dirty = state.committed === null || !same(draft, state.committed);
  return { ...state, draft, dirty, status: state.status === 'saving' ? 'saving' : 'idle' };
}

function submittable<T>(state: InspectorState<T>): boolean {
  return state.selectedId !== null && state.draft !== null && state.dirty && state.status !== 'saving';
}

export function requestSave<T>(state: InspectorState<T>, requestId: string): InspectorState<T> {
  if (!submittable(state)) {
    return state;
  }
  const selectedId = state.selectedId as string;
  const draft = state.draft as T;
  return {
    ...state,
    status: 'saving',
    submitted: { resourceId: selectedId, requestId, snapshot: clone(draft) }
  };
}

export function resolveSave<T>(
  state: InspectorState<T>,
  requestId: string,
  outcome: SaveOutcome<T>
): InspectorState<T> {
  if (!state.submitted || state.submitted.requestId !== requestId) return state;
  const snapshot = state.submitted.snapshot;
  if (outcome.type === 'success') {
    const committed = outcome.committed ?? snapshot;
    const draft = state.draft ?? committed;
    const dirty = !same(draft, committed);
    return {
      ...state,
      committed,
      draft,
      dirty,
      status: dirty ? 'idle' : 'saved',
      submitted: undefined
    };
  }
  return { ...state, status: 'error', submitted: undefined };
}
