// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { UiContextAction, UiContextTone, UiCrumb, UiTopbarContext } from './nav-types';

const MAX_VISIBLE_SCOPES = 3;

export function topbarClasses(options: {
  showBrand?: boolean;
  context?: UiTopbarContext;
  className?: string;
}): string {
  const { showBrand = true, context, className = '' } = options;
  return [
    'bb-topbar',
    !showBrand && 'bb-topbar--brandless',
    context && 'bb-topbar--ctx',
    context && `bb-topbar--ctx-${context.tone}`,
    className || null,
  ]
    .filter(Boolean)
    .join(' ');
}

export function topbarCrumbs(crumbs: UiCrumb[], context?: UiTopbarContext): UiCrumb[] {
  if (!context || crumbs.length <= 1) return crumbs;
  return crumbs.slice(-1);
}

export function visibleScopes(scopes?: string[]): { visible: string[]; overflow: number } {
  const list = scopes ?? [];
  if (list.length <= MAX_VISIBLE_SCOPES) return { visible: list, overflow: 0 };
  return { visible: list.slice(0, MAX_VISIBLE_SCOPES), overflow: list.length - MAX_VISIBLE_SCOPES };
}

export function contextActionClass(action: UiContextAction, fallback: UiContextTone): string {
  const tone = action.tone ?? fallback;
  return [
    'bb-topbar-ctx__btn',
    `bb-topbar-ctx__btn--${tone}`,
    action.subtle && 'bb-topbar-ctx__btn--subtle',
    action.compactLabel && 'bb-topbar-ctx__btn--has-compact',
  ]
    .filter(Boolean)
    .join(' ');
}
