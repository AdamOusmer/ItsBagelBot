// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type RovingOrientation = 'vertical' | 'horizontal' | 'both';

export interface RovingFocusOptions {
  selector: string;
  orientation?: RovingOrientation;
  wrap?: boolean;
}

const FORWARD: Record<RovingOrientation, readonly string[]> = {
  vertical: ['ArrowDown'],
  horizontal: ['ArrowRight'],
  both: ['ArrowDown', 'ArrowRight'],
};

const BACKWARD: Record<RovingOrientation, readonly string[]> = {
  vertical: ['ArrowUp'],
  horizontal: ['ArrowLeft'],
  both: ['ArrowUp', 'ArrowLeft'],
};

const TEXT_ENTRY = 'input, textarea, select, [contenteditable]:not([contenteditable="false"])';

export function stepFor(key: string, orientation: RovingOrientation = 'vertical'): -1 | 0 | 1 {
  if (FORWARD[orientation].includes(key)) return 1;
  if (BACKWARD[orientation].includes(key)) return -1;
  return 0;
}

function edgeIndex(key: string, count: number): number | null {
  if (key === 'Home') return 0;
  if (key === 'End') return count - 1;
  return null;
}

function stepped(current: number, step: -1 | 1, count: number, wrap: boolean): number {
  if (current < 0) return 0;
  if (wrap) return (current + step + count) % count;
  return Math.min(count - 1, Math.max(0, current + step));
}

export function nextIndex(
  key: string,
  current: number,
  count: number,
  options: Omit<RovingFocusOptions, 'selector'> = {},
): number | null {
  if (count === 0) return null;
  const edge = edgeIndex(key, count);
  if (edge !== null) return edge;
  const step = stepFor(key, options.orientation);
  if (step === 0) return null;
  return stepped(current, step, count, options.wrap ?? false);
}

export function rovingItems(root: ParentNode, selector: string): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(selector)).filter((item) => !item.matches(':disabled'));
}

function isTextEntry(target: EventTarget | null): boolean {
  return (target as Element | null)?.matches?.(TEXT_ENTRY) ?? false;
}

function entersFromOutside(key: string, target: EventTarget | null): boolean {
  return key === 'ArrowDown' || !isTextEntry(target);
}

export function rovingTarget(root: ParentNode, event: KeyboardEvent, options: RovingFocusOptions): HTMLElement | null {
  const items = rovingItems(root, options.selector);
  const target = event.target as Node | null;
  const current = items.findIndex((item) => item.contains(target));
  if (current < 0 && !entersFromOutside(event.key, event.target)) return null;
  const index = nextIndex(event.key, current, items.length, options);
  return index === null ? null : items[index];
}

export function mountRovingFocus(root: HTMLElement, options: RovingFocusOptions): () => void {
  const onKeydown = (event: KeyboardEvent) => {
    const next = rovingTarget(root, event, options);
    if (!next) return;
    event.preventDefault();
    next.focus();
  };
  root.addEventListener('keydown', onKeydown);
  return () => root.removeEventListener('keydown', onKeydown);
}
