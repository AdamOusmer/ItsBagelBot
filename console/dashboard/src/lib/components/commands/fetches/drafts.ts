// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// sessionStorage-backed fetch-definition drafts — the bb-fetch-draft twin of
// the command editor's bb-cmd-draft mirror (drafts.ts): work in progress
// survives a stray navigation or refresh; a deliberate discard clears it.
import type { FetchKind } from '@bagel/shared';

// The editor's working copy of one definition (create + edit share the shape).
export interface FetchDraft {
  edit: boolean;
  /** Freeform input the slug derives from; the stored name is `name`. */
  displayName: string;
  name: string;
  originalName: string;
  url: string;
  kind: FetchKind;
  /** Path segments; [] for plain defs and root-scalar json picks. */
  path: string[];
  /** Stored key label; '' = keyless. */
  key_label: string;
  is_active: boolean;
  /** The rehearsal template: what a command embedding {urlfetch:<name>} might
   * render. Authoring aid only — never saved to the definition itself. */
  template: string;
}

const PREFIX = 'bb-fetch-draft:';

function draftKey(originalName: string, edit: boolean): string {
  return `${PREFIX}${edit ? originalName : 'new'}`;
}

export function loadFetchDraft(originalName: string, edit: boolean): FetchDraft | null {
  try {
    const raw = sessionStorage.getItem(draftKey(originalName, edit));
    return raw ? (JSON.parse(raw) as FetchDraft) : null;
  } catch {
    return null;
  }
}

export function saveFetchDraft(draft: FetchDraft): void {
  try {
    sessionStorage.setItem(draftKey(draft.originalName, draft.edit), JSON.stringify(draft));
  } catch {
    /* storage full/unavailable — drafts are best-effort */
  }
}

export function clearFetchDraft(originalName: string, edit: boolean): void {
  try {
    sessionStorage.removeItem(draftKey(originalName, edit));
  } catch {
    /* best-effort */
  }
}
