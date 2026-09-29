// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const ITEMS = '[role="menuitem"]';

function target(items: HTMLElement[], key: string, from: number): HTMLElement | undefined {
  const last = items.length - 1;
  if (key === 'Home') return items[0];
  if (key === 'End') return items[last];
  if (key === 'ArrowDown') return items[from >= last ? 0 : from + 1];
  return items[from <= 0 ? last : from - 1];
}

export function menuKeys(node: HTMLElement, onTab: () => void) {
  const items = () => [...node.querySelectorAll<HTMLElement>(ITEMS)];
  items()[0]?.focus();

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Tab') return onTab();
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    const list = items();
    event.preventDefault();
    target(list, event.key, list.indexOf(document.activeElement as HTMLElement))?.focus();
  }

  node.addEventListener('keydown', onKeydown);
  return { destroy: () => node.removeEventListener('keydown', onKeydown) };
}
