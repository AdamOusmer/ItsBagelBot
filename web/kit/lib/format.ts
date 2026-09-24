// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function minutesSince(iso: string): number {
  return Math.max(Math.round((Date.now() - new Date(iso).getTime()) / 60e3), 0);
}

export function ago(iso?: string | null, missing = '-'): string {
  if (!iso) return missing;
  const mins = minutesSince(iso);
  if (mins < 1) return 'now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 48) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

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

export function fmtDateTime(iso: string | null | undefined, missing = 'unknown'): string {
  if (!iso) return missing;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return missing;
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: 'numeric', timeZoneName: 'short'
  }).format(date);
}

export function fmtUtcDay(iso: string): string {
  const [y, m, d] = iso.split('-').map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC'
  });
}
