// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// #221: editing a command must not change whether it is enabled. The inspector
// draft used to snapshot Active at open, so a content Save wrote that snapshot
// back — a disabled command came back on, and a row toggle with the inspector
// open reverted on save. Content writes take the live row; only Create (no
// live row yet) takes the draft checkbox.

export function persistCommandActive(
  edit: boolean,
  draftActive: boolean,
  liveActive: boolean | undefined
): boolean {
  if (!edit) return draftActive;
  return liveActive ?? draftActive;
}

export function overlayLiveActive<T extends { is_active: boolean }>(draft: T, liveActive: boolean): T {
  if (draft.is_active === liveActive) return draft;
  return { ...draft, is_active: liveActive };
}

// Dirty / sessionStorage compare without Active: a live toggle must not flag
// the editor unsaved or persist a draft that only differs in is_active.
export function commandContentSnapshot(draft: object): string {
  const { is_active: _ignored, ...content } = draft as { is_active?: unknown };
  return JSON.stringify(content);
}
