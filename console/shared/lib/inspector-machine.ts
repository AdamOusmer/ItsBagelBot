// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Pure master-detail inspector state machine, framework-free so it can be
// unit-tested without a component harness; a thin Svelte wrapper
// (dashboard/src/lib/inspector/inspector.svelte.ts) holds it in $state for the
// components.
//
// What it is FOR is the stale-response guard in resolveSave: an inspector's
// async save callback used to read the CURRENT global selection, so saving row
// A and then opening row B let A's response mutate or close B. Here a response
// is applied only while its requestId still matches the submission in flight;
// a response for a since-abandoned or since-switched selection is a no-op.
//
// It was built for five routes and one adopted it (timers); the other four keep
// their drafts in $state with their own isDirty and park interrupted actions in
// createDiscardGuard, which holds an arbitrary callback rather than the closed
// set of intents this used to model. So the intent-parking and external-update
// halves went, unused, along with the 'conflict' status they fed: the pages
// that would raise one do not poll. Re-add them WITH the caller that needs
// them; what stays is the part that had a bug to prevent.

export type InspectorStatus = 'idle' | 'saving' | 'saved' | 'error';

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
};

export type SaveOutcome<T> =
  | { type: 'success'; committed?: T } // server truth, if it differs from the snapshot
  | { type: 'error' };

// Drafts are plain JSON data, but at runtime they arrive wrapped in Svelte 5
// $state proxies, which structuredClone rejects (DataCloneError: a Proxy has no
// clonable internal slots). Fall back to JSON cloning for those.
const clone = <T>(v: T): T => {
  if (typeof structuredClone === 'function') {
    try {
      return structuredClone(v);
    } catch {
      // proxied state, clone via JSON below
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
export function requestSave<T>(state: InspectorState<T>, requestId: string): InspectorState<T> {
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
  // error: keep the draft, surface the failure.
  return { ...state, status: 'error', submitted: undefined };
}
