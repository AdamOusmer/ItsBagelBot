// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export function commandContentSnapshot(draft: object): string {
  const { is_active: _ignored, ...content } = draft as { is_active?: unknown };
  return JSON.stringify(content);
}
