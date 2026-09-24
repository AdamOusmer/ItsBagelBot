// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { IconName } from './icons';
import type { UiNavGroup, UiNavLink } from './nav-types';

export function isGrouped(groups: UiNavGroup[]): boolean {
  return groups.length > 1;
}

export function hoistHome(groups: UiNavGroup[], homeHref = '/'): UiNavLink | null {
  return groups.flatMap((g) => g.items).find((i) => i.href === homeHref) ?? null;
}

export function dockGroups(groups: UiNavGroup[], homeHref = '/'): UiNavGroup[] {
  return groups
    .map((g) => ({ ...g, items: g.items.filter((i) => i.href !== homeHref) }))
    .filter((g) => g.items.length > 0);
}

export function groupActive(group: UiNavGroup): boolean {
  return group.items.some((i) => i.active);
}

export function groupCount(group: UiNavGroup): number | undefined {
  return group.items.reduce((n, i) => n + (Number(i.count) || 0), 0) || undefined;
}

export function groupIcon(group: UiNavGroup, fallback?: IconName): IconName | undefined {
  return group.items[0]?.icon ?? fallback;
}
