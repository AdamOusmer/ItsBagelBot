// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The dock's grouping arithmetic, as four pure functions.
//
// They were `$derived` expressions and inline helpers inside the component,
// which made the rule they encode -- "a board with more than one nav group
// collapses each group into one button, except the one routed at /, which is
// hoisted out as Home" -- untestable and invisible. It is the only piece of
// this shell that is a decision rather than a layout, and it is what keeps the
// dock at a handful of buttons no matter how many routes a board grows.
//
// Pure and framework-free on purpose: the Svelte adapter calls them from
// `$derived`, the Astro adapter calls them at build time, and the unit test
// calls them with plain arrays.

import type { IconName } from './icons';
import type { UiNavGroup, UiNavLink } from './nav-types';

/** Grouped mode at all: one group is a flat dock, several is a folded one. */
export function isGrouped(groups: UiNavGroup[]): boolean {
  return groups.length > 1;
}

/**
 * The item routed at `homeHref`, wherever it sits. Hoisted out of its group so
 * the way back is always one tap and never behind a popover.
 */
export function hoistHome(groups: UiNavGroup[], homeHref = '/'): UiNavLink | null {
  return groups.flatMap((g) => g.items).find((i) => i.href === homeHref) ?? null;
}

/**
 * The groups the dock renders, with the hoisted home item removed and any
 * group it emptied dropped -- an empty group would otherwise render a button
 * that opens a popover with nothing in it.
 */
export function dockGroups(groups: UiNavGroup[], homeHref = '/'): UiNavGroup[] {
  return groups
    .map((g) => ({ ...g, items: g.items.filter((i) => i.href !== homeHref) }))
    .filter((g) => g.items.length > 0);
}

/** A folded group is current when any page inside it is. */
export function groupActive(group: UiNavGroup): boolean {
  return group.items.some((i) => i.active);
}

/**
 * The badge on a folded group: the sum of its items' counts, or undefined when
 * that sum is zero. `Number(i.count) || 0` rather than a cast because counts
 * are rendered verbatim and may legitimately be strings ("9+").
 */
export function groupCount(group: UiNavGroup): number | undefined {
  return group.items.reduce((n, i) => n + (Number(i.count) || 0), 0) || undefined;
}

/**
 * The glyph on a folded group's button: the first item's, or the caller's
 * fallback.
 *
 * `fallback` is a parameter with NO default, which is the change from the
 * version this replaces. That one ended `?? 'overview'` -- an icon named by
 * the bot's own set, hard-coded inside what is now a library, so a consumer
 * with a different set would have rendered a missing glyph. It also carried a
 * `GROUP_ICONS` lookup that was an empty Record: dead code with a live
 * fallback chain hanging off it.
 */
export function groupIcon(group: UiNavGroup, fallback?: IconName): IconName | undefined {
  return group.items[0]?.icon ?? fallback;
}
