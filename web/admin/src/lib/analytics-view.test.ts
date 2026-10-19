// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { buildAnalytics } from './analytics-page';
import { cohorts, isoWeek, weekStart, type AnalyticsRow } from './analytics-view';

const row = (start: string, metric: string, dims: string[], value: number, suppressed = false): AnalyticsRow => ({
  start,
  metric,
  dims,
  value,
  suppressed
});

describe('analytics view', () => {
  it('matches Go ISO weeks across a year boundary', () => {
    expect(isoWeek('2026-12-31')).toBe('2026-W53');
    expect(isoWeek('2027-01-04')).toBe('2027-W01');
    expect(weekStart('2026-W42')).toBe('2026-10-12');
    expect(weekStart('2027-W01')).toBe('2027-01-04');
  });

  it('places cohort cells by weeks since signup and sizes them from signup counts', () => {
    const days = [row('2026-10-12', 'lifecycle', ['signup'], 12), row('2026-10-14', 'lifecycle', ['signup'], 8)];
    const weeks = [
      row('2026-10-12', 'cohort.active', ['2026-W42'], 18),
      row('2026-10-26', 'cohort.active', ['2026-W42'], 0, true)
    ];
    expect(cohorts(weeks, days, 3)).toEqual([
      {
        week: '2026-W42',
        size: { value: 20, partial: false },
        cells: [{ value: 18, partial: false }, null, { value: 0, partial: true }]
      }
    ]);
  });

  it('shows suppressed cells as <10 and partial sums with a plus', () => {
    const day = [
      row('2026-10-17', 'accounts.total', ['free'], 40),
      row('2026-10-17', 'accounts.total', ['vip'], 0, true),
      row('2026-10-17', 'channels.active', [], 25)
    ];
    const month = [row('2026-10-01', 'channels.active', [], 50)];
    const view = buildAnalytics({ day, week: [], month, cohortWeeks: [], cohortDays: [] });
    const tile = (key: string) => view.tiles.find((t) => t.key === key)?.value;

    expect(tile('accounts')).toBe('40+');
    expect(tile('paying')).toBe('<10');
    expect(tile('stickiness')).toBe('50%');
    expect(view.tables.find((t) => t.key === 'tiers')?.rows).toEqual([
      { label: 'free', values: ['40', '0'] },
      { label: 'paid', values: ['0', '0'] },
      { label: 'vip', values: ['<10', '0'] }
    ]);
  });
});
