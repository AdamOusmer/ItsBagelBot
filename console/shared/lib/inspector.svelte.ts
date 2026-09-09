// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Reactive Svelte wrapper over the pure inspector-machine: holds the machine
// state in $state, mints the request ids, and exposes the intents. The safety
// logic lives in the machine (unit-tested there); this is only the shell.
//
// Lives beside the machine in shared/lib rather than in one app's $lib: the
// admin console's draft pages need the same shell, and a copy of a state
// machine's wrapper is exactly the thing that drifts. The .svelte.ts suffix
// marks it as a runes module, so the plain .ts files around it stay
// framework-free and unit-testable without a component harness.
//
// One route drives an inspector through this (timers). The other draft pages
// keep their own $state and park interrupted actions in createDiscardGuard,
// which is why the guarded-navigation methods this used to expose went with
// the machine halves behind them.
import {
  initial,
  openClean,
  edit as mEdit,
  requestSave,
  resolveSave,
  type InspectorState,
  type SaveOutcome
} from './inspector-machine';

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

    // Begin a save: mints a request id and captures the immutable snapshot.
    // Returns null when there is nothing dirty to save.
    beginSave(): { requestId: string; snapshot: T } | null {
      const requestId = `req-${++seq}`;
      const next = requestSave(state, requestId);
      if (next === state) return null;
      state = next;
      return { requestId, snapshot: state.submitted!.snapshot };
    },
    // Apply a response. A no-op if the selection has since moved on (the machine
    // matches on requestId), so a late response for A can never mutate B. Returns
    // whether it actually applied, so callers can gate side effects (e.g. closing
    // the inspector after a create) on the response still being relevant.
    resolved(requestId: string, outcome: SaveOutcome<T>): boolean {
      const before = state;
      state = resolveSave(state, requestId, outcome);
      return state !== before;
    }
  };
}

export type Inspector<T> = ReturnType<typeof createInspector<T>>;
