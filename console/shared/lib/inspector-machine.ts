// Pure master-detail inspector state machine. Five routes each reimplemented an
// inspector with subtly different, unsafe behaviour: drafts were dropped on
// close/Escape/row-switch, async save callbacks read the *current* global
// selection (so saving A then opening B let A's response mutate or close B), and
// an external update could silently clobber an in-progress edit. This encodes one
// contract, framework-free so it can be unit-tested exhaustively; a thin Svelte
// wrapper holds it in $state for the components.
//
// The core safety property lives in resolveSave: a response is applied only if
// its requestId still matches the in-flight submission. Anything else (a stale
// response for a since-abandoned or since-switched selection) is a no-op.

export type InspectorStatus = 'idle' | 'saving' | 'saved' | 'error' | 'conflict';

// Captured when a dirty draft interrupts navigation, so the discard guard can
// resume exactly where the user was headed once they choose.
export type PendingIntent =
  | { kind: 'close' }
  | { kind: 'select'; id: string }
  | { kind: 'navigate'; to: string };

export type InspectorState<T> = {
  selectedId: string | null;
  committed: T | null; // last known server truth for the selection
  draft: T | null; // working copy being edited
  dirty: boolean;
  status: InspectorStatus;
  // Present while a save is in flight: the immutable identity + snapshot of what
  // was submitted. A late response is matched against requestId and ignored if it
  // no longer applies.
  submitted?: { resourceId: string; requestId: string; snapshot: T };
  // Set when a dirty draft blocks a close/select/navigate until the user decides.
  pendingIntent?: PendingIntent;
  // Set when an external update arrives for the selected item while it is dirty.
  conflictWith?: T;
};

export type SaveOutcome<T> =
  | { type: 'success'; committed?: T } // server truth, if it differs from the snapshot
  | { type: 'error' }
  | { type: 'conflict'; committed?: T };

// Drafts are plain JSON data, but at runtime they arrive wrapped in Svelte 5
// $state proxies, which structuredClone rejects (DataCloneError: a Proxy has no
// clonable internal slots). Fall back to JSON cloning for those.
const clone = <T>(v: T): T => {
  if (typeof structuredClone === 'function') {
    try {
      return structuredClone(v);
    } catch {
      // proxied state — clone via JSON below
    }
  }
  return JSON.parse(JSON.stringify(v)) as T;
};

// Default dirtiness check: deep-equal by JSON. Drafts here are plain data;
// unknown params so a possibly-null draft compares without generic friction.
const same = (a: unknown, b: unknown): boolean => JSON.stringify(a) === JSON.stringify(b);

export function initial<T>(): InspectorState<T> {
  return { selectedId: null, committed: null, draft: null, dirty: false, status: 'idle' };
}

// Open a selection cleanly: draft starts as a copy of committed truth.
export function openClean<T>(id: string, committed: T): InspectorState<T> {
  return {
    selectedId: id,
    committed,
    draft: clone(committed),
    dirty: false,
    status: 'idle'
  };
}

// Edit the working draft. Dirtiness is recomputed against committed, so undoing a
// change back to the original clears dirty. A prior 'saved'/'error' flag resets.
export function edit<T>(state: InspectorState<T>, draft: T): InspectorState<T> {
  const dirty = state.committed === null || !same(draft, state.committed);
  return { ...state, draft, dirty, status: state.status === 'saving' ? 'saving' : 'idle' };
}

// submittable is true when there is a dirty selection with a draft and no save
// already in flight.
function submittable<T>(state: InspectorState<T>): boolean {
  return state.selectedId !== null && state.draft !== null && state.dirty && state.status !== 'saving';
}

// Begin a save. No-op unless there is a dirty selection. Captures the immutable
// (resourceId, requestId, snapshot) so the response can be matched later.
export function requestSave<T>(
  state: InspectorState<T>,
  requestId: string
): InspectorState<T> {
  if (!submittable(state)) {
    return state;
  }
  // submittable guarantees selectedId and draft are set.
  const selectedId = state.selectedId as string;
  const draft = state.draft as T;
  return {
    ...state,
    status: 'saving',
    submitted: { resourceId: selectedId, requestId, snapshot: clone(draft) }
  };
}

// Apply a save response. THE guard: ignore anything whose requestId is not the
// one currently in flight (a response for a selection the user has since left).
// On success the inspector stays open and clean (Save does not close it).
export function resolveSave<T>(
  state: InspectorState<T>,
  requestId: string,
  outcome: SaveOutcome<T>
): InspectorState<T> {
  if (!state.submitted || state.submitted.requestId !== requestId) return state;
  const snapshot = state.submitted.snapshot;
  if (outcome.type === 'success') {
    const committed = outcome.committed ?? snapshot;
    // Only settle to clean if the user hasn't edited further since submitting.
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
  if (outcome.type === 'conflict') {
    return { ...state, status: 'conflict', conflictWith: outcome.committed, submitted: undefined };
  }
  // error: keep the draft, surface the failure.
  return { ...state, status: 'error', submitted: undefined };
}

// Guarded navigation. If the draft is dirty, capture the intent and let the caller
// raise a discard confirmation; otherwise the caller may proceed immediately.
export function requestClose<T>(state: InspectorState<T>): InspectorState<T> {
  if (state.dirty) return { ...state, pendingIntent: { kind: 'close' } };
  return initial<T>();
}

export function requestSelect<T>(state: InspectorState<T>, id: string): InspectorState<T> {
  if (state.dirty && id !== state.selectedId) {
    return { ...state, pendingIntent: { kind: 'select', id } };
  }
  return state; // caller loads committed for `id` and calls openClean
}

export function requestNavigate<T>(state: InspectorState<T>, to: string): InspectorState<T> {
  if (state.dirty) return { ...state, pendingIntent: { kind: 'navigate', to } };
  return state;
}

// User kept editing: drop the pending intent, leave the draft untouched.
export function cancelIntent<T>(state: InspectorState<T>): InspectorState<T> {
  return { ...state, pendingIntent: undefined };
}

// User confirmed discard: returns the resumed intent for the caller to execute
// (close the panel, load another row, leave the page) plus the cleared state.
export function confirmDiscard<T>(
  state: InspectorState<T>
): { state: InspectorState<T>; intent: PendingIntent | null } {
  const intent = state.pendingIntent ?? null;
  return { state: initial<T>(), intent };
}

// An external update (SSE/poll) for some item. If it is not the selected item, or
// the selection is clean, rebase onto it. If the selected item is dirty, raise a
// conflict rather than clobbering the user's edit.
export function externalUpdate<T>(
  state: InspectorState<T>,
  id: string,
  committed: T
): InspectorState<T> {
  if (state.selectedId !== id) return state;
  if (!state.dirty) return { ...state, committed, draft: clone(committed) };
  return { ...state, status: 'conflict', conflictWith: committed };
}

// Resolve a conflict: 'take' adopts the external version (discards the local
// edit); 'keep' keeps editing against the new committed base (still dirty).
export function resolveConflict<T>(
  state: InspectorState<T>,
  choice: 'take' | 'keep'
): InspectorState<T> {
  const incoming = state.conflictWith;
  if (incoming === undefined) return { ...state, status: 'idle' };
  if (choice === 'take') {
    return { ...state, committed: incoming, draft: clone(incoming), dirty: false, status: 'idle', conflictWith: undefined };
  }
  const dirty = state.draft === null || !same(state.draft, incoming);
  return { ...state, committed: incoming, dirty, status: 'idle', conflictWith: undefined };
}
