// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { UiCrumb, UiTopbarContext } from './nav-types';

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
