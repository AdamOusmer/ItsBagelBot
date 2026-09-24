// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  initial,
  openClean,
  edit as mEdit,
  requestSave,
  resolveSave,
  type InspectorState,
  type SaveOutcome
} from '../lib/inspector-machine';

export function createInspector<T>() {
  let state = $state<InspectorState<T>>(initial<T>());
  let seq = 0;

  return {
    get state() {
      return state;
    },
    get selectedId() {
      return state.selectedId;
    },
    get draft() {
      return state.draft;
    },
    get dirty() {
      return state.dirty;
    },
    get status() {
      return state.status;
    },
    get isOpen() {
      return state.selectedId !== null;
    },
    open(id: string, committed: T) {
      state = openClean(id, committed);
    },
    edit(draft: T) {
      state = mEdit(state, draft);
    },
    reset() {
      state = initial<T>();
    },

    beginSave(): { requestId: string; snapshot: T } | null {
      const requestId = `req-${++seq}`;
      const next = requestSave(state, requestId);
      if (next === state) return null;
      state = next;
      return { requestId, snapshot: state.submitted!.snapshot };
    },
    resolved(requestId: string, outcome: SaveOutcome<T>): boolean {
      const before = state;
      state = resolveSave(state, requestId, outcome);
      return state !== before;
    }
  };
}

export type Inspector<T> = ReturnType<typeof createInspector<T>>;
