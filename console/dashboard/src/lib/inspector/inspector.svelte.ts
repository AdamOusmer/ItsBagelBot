// Reactive Svelte wrapper over the pure inspector-machine. Holds the machine
// state in $state and exposes intent methods; every route inspector (timers
// first, then commands/modules/channel points/govee) drives selection, dirty
// tracking, and stale-safe saves through one of these instead of hand-rolling
// its own. The safety logic lives in the machine (unit-tested); this is only the
// reactive shell + request-id minting.
import {
  initial,
  openClean,
  edit as mEdit,
  requestSave,
  resolveSave,
  requestClose,
  requestSelect,
  cancelIntent,
  confirmDiscard,
  externalUpdate,
  type InspectorState,
  type SaveOutcome,
  type PendingIntent
} from '@bagel/shared';

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
    get pendingIntent(): PendingIntent | undefined {
      return state.pendingIntent;
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

    // Guarded navigation. Each returns the parked intent (truthy => the caller
    // should raise a discard confirmation instead of proceeding).
    requestClose(): PendingIntent | undefined {
      state = requestClose(state);
      return state.pendingIntent;
    },
    requestSelect(id: string): PendingIntent | undefined {
      state = requestSelect(state, id);
      return state.pendingIntent;
    },
    cancelIntent() {
      state = cancelIntent(state);
    },
    confirmDiscard(): PendingIntent | null {
      const r = confirmDiscard(state);
      state = r.state;
      return r.intent;
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
    // matches on requestId), so a late response for A can never mutate B.
    resolved(requestId: string, outcome: SaveOutcome<T>) {
      state = resolveSave(state, requestId, outcome);
    },
    externalUpdate(id: string, committed: T) {
      state = externalUpdate(state, id, committed);
    }
  };
}

export type Inspector<T> = ReturnType<typeof createInspector<T>>;
