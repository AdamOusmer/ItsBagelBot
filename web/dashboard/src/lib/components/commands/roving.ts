// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export interface RovingHandlers {
  onSpace: (name: string) => void;
  onDelete: (name: string) => void;
}

const ITEM = '.bb-row__primary';
const SECONDARY = '.bb-row__actions button, .bb-row__actions a, .bb-row__actions input';

const rowName = (el: Element) => el.closest<HTMLElement>('[data-row-name]')?.dataset.rowName ?? '';

export function rovingList(node: HTMLElement, initial: RovingHandlers) {
  let handlers = initial;
  let active: HTMLElement | null = null;

  const items = () => Array.from(node.querySelectorAll<HTMLElement>(ITEM));

  function sync() {
    const all = items();
    if (!active || !all.includes(active)) active = all[0] ?? null;
    for (const el of all) el.tabIndex = el === active ? 0 : -1;
    for (const el of node.querySelectorAll<HTMLElement>(SECONDARY)) el.tabIndex = -1;
  }

  function moveTo(el: HTMLElement | undefined) {
    if (!el) return;
    active = el;
    sync();
    el.focus();
  }

  const MOVES: Record<string, (all: HTMLElement[], at: number) => HTMLElement | undefined> = {
    ArrowDown: (all, at) => all[Math.min(all.length - 1, at + 1)],
    ArrowUp: (all, at) => all[Math.max(0, at - 1)],
    Home: (all) => all[0],
    End: (all) => all[all.length - 1]
  };

  function isPlainKeyOnItem(e: KeyboardEvent, target: HTMLElement): boolean {
    return target.matches(ITEM) && !e.metaKey && !e.ctrlKey && !e.altKey;
  }

  function onKeydown(e: KeyboardEvent) {
    const target = e.target as HTMLElement;
    if (!isPlainKeyOnItem(e, target)) return;
    const move = MOVES[e.key];
    if (move) {
      e.preventDefault();
      const all = items();
      moveTo(move(all, all.indexOf(target)));
    } else if (e.key === ' ') {
      e.preventDefault();
      handlers.onSpace(rowName(target));
    } else if (e.key === 'Delete') {
      e.preventDefault();
      handlers.onDelete(rowName(target));
    }
  }

  function onKeyup(e: KeyboardEvent) {
    if (e.key === ' ' && (e.target as HTMLElement).matches(ITEM)) e.preventDefault();
  }

  function onFocusin(e: FocusEvent) {
    const target = e.target as HTMLElement;
    if (!target.matches(ITEM) || target === active) return;
    active = target;
    sync();
  }

  const observer = new MutationObserver(() => queueMicrotask(sync));
  observer.observe(node, { childList: true, subtree: true });
  node.addEventListener('keydown', onKeydown);
  node.addEventListener('keyup', onKeyup);
  node.addEventListener('focusin', onFocusin);
  sync();

  return {
    update(next: RovingHandlers) {
      handlers = next;
    },
    destroy() {
      observer.disconnect();
      node.removeEventListener('keydown', onKeydown);
      node.removeEventListener('keyup', onKeyup);
      node.removeEventListener('focusin', onFocusin);
    }
  };
}
