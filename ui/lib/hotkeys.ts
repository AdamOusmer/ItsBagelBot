// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type ShortcutChord = { alt?: boolean };

const TYPING_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT']);

export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el) return false;
  return TYPING_TAGS.has(el.tagName) || el.isContentEditable === true;
}

export function isTyping(event: Event): boolean {
  return isTypingTarget(event.target);
}

export function isShortcut(event: KeyboardEvent, chord: ShortcutChord = {}): boolean {
  if (isTyping(event) || event.metaKey || event.ctrlKey) return false;
  return event.altKey === (chord.alt ?? false);
}
