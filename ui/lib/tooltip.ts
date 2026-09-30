// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { FOCUSABLE } from './overlay-stack';

export const TOOLTIP_DISMISSED = 'data-dismissed';

function addToken(el: HTMLElement, name: string, token: string): () => void {
  const tokens = (el.getAttribute(name) ?? '').split(/\s+/).filter(Boolean);
  if (!tokens.includes(token)) el.setAttribute(name, [...tokens, token].join(' '));
  return () => {
    const rest = (el.getAttribute(name) ?? '').split(/\s+/).filter((t) => t && t !== token);
    if (rest.length) el.setAttribute(name, rest.join(' '));
    else el.removeAttribute(name);
  };
}

export function wireTooltip(root: HTMLElement, bubble: HTMLElement): () => void {
  const trigger = root.querySelector<HTMLElement>(FOCUSABLE);
  const unlink = trigger && bubble.id ? addToken(trigger, 'aria-describedby', bubble.id) : undefined;

  const shown = () => !root.hasAttribute(TOOLTIP_DISMISSED) && root.matches(':hover, :focus-within');
  const reset = () => root.removeAttribute(TOOLTIP_DISMISSED);

  const onKeydown = (event: KeyboardEvent) => {
    if (event.key !== 'Escape' || !shown()) return;
    event.stopImmediatePropagation();
    root.setAttribute(TOOLTIP_DISMISSED, '');
  };
  const onPointerLeave = () => {
    if (!root.matches(':focus-within')) reset();
  };
  const onFocusOut = (event: FocusEvent) => {
    if (!root.matches(':hover') && !root.contains(event.relatedTarget as Node | null)) reset();
  };

  document.addEventListener('keydown', onKeydown, true);
  root.addEventListener('pointerleave', onPointerLeave);
  root.addEventListener('focusout', onFocusOut);
  return () => {
    document.removeEventListener('keydown', onKeydown, true);
    root.removeEventListener('pointerleave', onPointerLeave);
    root.removeEventListener('focusout', onFocusOut);
    unlink?.();
    reset();
  };
}
