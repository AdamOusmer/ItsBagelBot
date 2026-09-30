// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type MenuAlign = 'start' | 'end';

export interface MenuPlacement {
  x: number;
  y: number;
  side: 'top' | 'bottom';
  maxHeight: number;
}

const GAP = 8;
const EDGE = 8;

export function placeMenu(
  trigger: Pick<DOMRect, 'top' | 'bottom' | 'left' | 'right'>,
  menu: Pick<DOMRect, 'width' | 'height'>,
  viewport: { width: number; height: number },
  align: MenuAlign,
): MenuPlacement {
  const below = viewport.height - trigger.bottom - GAP - EDGE;
  const above = trigger.top - GAP - EDGE;
  const side = menu.height > below && above > below ? 'top' : 'bottom';
  const room = Math.max(0, side === 'bottom' ? below : above);
  const height = Math.min(menu.height, room);
  const preferred = align === 'end' ? trigger.right - menu.width : trigger.left;
  return {
    x: Math.max(EDGE, Math.min(preferred, viewport.width - EDGE - menu.width)),
    y: side === 'bottom' ? trigger.bottom + GAP : trigger.top - GAP - height,
    side,
    maxHeight: room,
  };
}

const supportsPopover = (menu: HTMLElement | null): menu is HTMLElement =>
  typeof menu?.showPopover === 'function';

const isCurrent = (item: HTMLElement) =>
  item.getAttribute('aria-current') === 'true' || item.getAttribute('aria-pressed') === 'true';

/** Anchors the `.bb-lang-switch` popover to its trigger and adds arrow-key movement. */
export function mountLangSwitch(root: HTMLElement): () => void {
  const trigger = root.querySelector<HTMLButtonElement>('.bb-lang-switch__trigger');
  const menu = root.querySelector<HTMLElement>('.bb-lang-switch__menu');
  if (!trigger || !supportsPopover(menu)) return () => {};

  const items = () => Array.from(menu.querySelectorAll<HTMLElement>('.bb-lang-switch__opt'));
  const isOpen = () => menu.matches(':popover-open');

  const place = () => {
    menu.style.maxHeight = '';
    const placement = placeMenu(
      trigger.getBoundingClientRect(),
      menu.getBoundingClientRect(),
      { width: document.documentElement.clientWidth, height: window.innerHeight },
      root.classList.contains('bb-lang-switch--field') ? 'start' : 'end',
    );
    menu.style.left = `${placement.x}px`;
    menu.style.top = `${placement.y}px`;
    menu.style.maxHeight = `${placement.maxHeight}px`;
    menu.dataset.side = placement.side;
    menu.dataset.anchored = '';
  };

  const open = (focus?: 'current') => {
    if (!isOpen()) menu.showPopover();
    place();
    if (focus) {
      const list = items();
      (list.find(isCurrent) ?? list[0])?.focus();
    }
  };

  const close = (refocus: boolean) => {
    if (!isOpen()) return;
    menu.hidePopover();
    if (refocus) trigger.focus({ preventScroll: true });
  };

  const onTriggerClick = (event: MouseEvent) => {
    event.preventDefault();
    if (isOpen()) close(false);
    else open();
  };

  const onTriggerKey = (event: KeyboardEvent) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
    event.preventDefault();
    open('current');
  };

  const onMenuKey = (event: KeyboardEvent) => {
    const list = items();
    const at = list.indexOf(document.activeElement as HTMLElement);
    const next =
      event.key === 'ArrowDown' ? list[(at + 1) % list.length]
      : event.key === 'ArrowUp' ? list[(at - 1 + list.length) % list.length]
      : event.key === 'Home' ? list[0]
      : event.key === 'End' ? list[list.length - 1]
      : undefined;
    if (!next) return;
    event.preventDefault();
    next.focus();
  };

  const onMenuClick = (event: MouseEvent) => {
    if ((event.target as Element | null)?.closest('.bb-lang-switch__opt')) close(true);
  };

  const onFocusOut = (event: FocusEvent) => {
    const next = event.relatedTarget as Node | null;
    if (next && !root.contains(next)) close(false);
  };

  const onViewport = (event: Event) => {
    if (event.target instanceof Node && menu.contains(event.target)) return;
    place();
  };

  const unfollow = () => {
    window.removeEventListener('scroll', onViewport, { capture: true });
    window.removeEventListener('resize', onViewport);
  };

  const onToggle = (event: Event) => {
    if ((event as ToggleEvent).newState !== 'open') return unfollow();
    window.addEventListener('scroll', onViewport, { capture: true, passive: true });
    window.addEventListener('resize', onViewport);
  };

  trigger.addEventListener('click', onTriggerClick);
  trigger.addEventListener('keydown', onTriggerKey);
  menu.addEventListener('keydown', onMenuKey);
  menu.addEventListener('click', onMenuClick);
  menu.addEventListener('toggle', onToggle);
  root.addEventListener('focusout', onFocusOut);

  return () => {
    close(false);
    unfollow();
    trigger.removeEventListener('click', onTriggerClick);
    trigger.removeEventListener('keydown', onTriggerKey);
    menu.removeEventListener('keydown', onMenuKey);
    menu.removeEventListener('click', onMenuClick);
    menu.removeEventListener('toggle', onToggle);
    root.removeEventListener('focusout', onFocusOut);
  };
}
