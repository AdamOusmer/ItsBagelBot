// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Relative and absolute time rendering for operator surfaces.
//
// `ago` existed four times in admin and had already drifted: the staff roster's
// copy was missing the `< 1 min -> 'now'` branch, so a just-granted admin read
// as "0m ago" while the same row on the overview read "now". That is the whole
// argument for one copy; the thresholds below are the union of what the four
// copies agreed on.
//
// Deliberately not Intl.RelativeTimeFormat: these strings are operator English
// in the admin console, which is not localized, and the buckets (minutes to
// 48h, then days) are not what RelativeTimeFormat would choose.

/** Whole minutes elapsed since `iso`, floored at 0 so clock skew never reads negative. */
function minutesSince(iso: string): number {
  return Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
}

/**
 * Compact relative age: `now`, `12m ago`, `5h ago` (to 48h), then `3d ago`.
 * A missing timestamp renders `missing` rather than "NaNm ago".
 */
export function ago(iso?: string | null, missing = '-'): string {
  if (!iso) return missing;
  const mins = minutesSince(iso);
  if (mins < 1) return 'now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

/** Absolute calendar date in the viewer's locale, for timestamps too old to read as an age. */
export function fmtDate(
  iso: string | null | undefined,
  opts: { missing?: string; parts?: Intl.DateTimeFormatOptions } = {}
): string {
  if (!iso) return opts.missing ?? 'unknown';
  return new Date(iso).toLocaleDateString(
    undefined,
    opts.parts ?? { year: 'numeric', month: 'short', day: 'numeric' }
  );
}

/**
 * A `YYYY-MM-DD` bucket label. Analytics days are UTC buckets, so they are
 * rendered as UTC too: parsing one as a local date drifts the label a day for
 * anyone west of Greenwich, which is how the same bar got two dates on two
 * charts.
 */
export function fmtUtcDay(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC'
  });
}
