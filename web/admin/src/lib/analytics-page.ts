// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  byDim,
  cellText,
  cohorts,
  daily,
  latest,
  share,
  total,
  type AnalyticsRow,
  type Cell
} from './analytics-view';

export const ANALYTICS_WINDOWS = [30, 90, 365] as const;

export type AnalyticsReads = {
  day: AnalyticsRow[];
  week: AnalyticsRow[];
  month: AnalyticsRow[];
  cohortWeeks: AnalyticsRow[];
  cohortDays: AnalyticsRow[];
};

export type Tile = { key: string; value: string };
export type Series = { key: string; values: number[] };
export type TableRow = { label: string; values: string[] };
export type Table = { key: string; columns: string[]; rows: TableRow[] };
export type CohortLine = { week: string; size: string; cells: string[] };
export type AnalyticsView = { tiles: Tile[]; series: Series[]; tables: Table[]; cohorts: CohortLine[]; empty: boolean };

function cellFor(rows: AnalyticsRow[], metric: string, match: (dims: string[]) => boolean): Cell {
  return total(rows.filter((r) => r.metric === metric && match(r.dims)), metric);
}

type ListSpec = { key: string; metric: string; index?: number; limit?: number };

function list(rows: AnalyticsRow[], spec: ListSpec): Table {
  return {
    key: spec.key,
    columns: ['count'],
    rows: byDim(rows, spec.metric, spec.index ?? 0, spec.limit ?? 50).map((l) => ({
      label: l.label,
      values: [cellText(l.cell)]
    }))
  };
}

type MatrixSpec = { key: string; metric: string; labelAt: number; colAt: number; cols: string[] };

function matrix(rows: AnalyticsRow[], spec: MatrixSpec): Table {
  const { metric, labelAt, colAt, cols } = spec;
  const labels = byDim(rows, metric, labelAt, 100).map((l) => l.label);
  return {
    key: spec.key,
    columns: cols,
    rows: labels.map((label) => ({
      label,
      values: cols.map((col) => cellText(cellFor(rows, metric, (d) => d[labelAt] === label && d[colAt] === col)))
    }))
  };
}

function tiles(r: AnalyticsReads): Tile[] {
  const dau = total(latest(r.day, 'channels.active'), 'channels.active');
  const mau = total(latest(r.month, 'channels.active'), 'channels.active');
  const accounts = latest(r.day, 'accounts.total');
  const paying = cellFor(accounts, 'accounts.total', (d) => d[0] !== 'free');
  const values: [string, string][] = [
    ['dau', cellText(dau)],
    ['wau', cellText(total(latest(r.week, 'channels.active'), 'channels.active'))],
    ['mau', cellText(mau)],
    ['stickiness', share(dau, mau)],
    ['accounts', cellText(total(accounts, 'accounts.total'))],
    ['paying', cellText(paying)],
    ['botEnabled', cellText(total(latest(r.day, 'accounts.bot_enabled'), 'accounts.bot_enabled'))],
    ['onboarded', cellText(total(latest(r.day, 'accounts.onboarded'), 'accounts.onboarded'))],
    ['cancelPending', cellText(total(latest(r.day, 'accounts.cancel_pending'), 'accounts.cancel_pending'))],
    ['paymentFailed', cellText(total(latest(r.day, 'accounts.payment_failed'), 'accounts.payment_failed'))],
    ['liveStarts', cellText(total(r.day, 'live.starts'))],
    ['timerFires', cellText(total(r.day, 'timer.fires'))]
  ];
  return values.map(([key, value]) => ({ key, value }));
}

function moduleTable(r: AnalyticsReads): Table {
  const weekly = latest(r.week, 'module.active');
  const names = new Set([
    ...byDim(r.day, 'module.events', 0, 200).map((l) => l.label),
    ...byDim(r.day, 'module.errors', 0, 200).map((l) => l.label),
    ...weekly.map((row) => row.dims[0])
  ]);
  const rows = [...names].sort().map((name) => {
    const runs = cellFor(r.day, 'module.latency', (d) => d[0] === name);
    const slow = cellFor(r.day, 'module.latency', (d) => d[0] === name && (d[1] === 'lt1s' || d[1] === 'ge1s'));
    const errors = cellFor(r.day, 'module.errors', (d) => d[0] === name);
    return {
      label: name,
      values: [
        cellText(cellFor(weekly, 'module.active', (d) => d[0] === name)),
        cellText(cellFor(r.day, 'module.events', (d) => d[0] === name)),
        cellText(errors),
        share(errors, { value: runs.value + errors.value, partial: runs.partial || errors.partial }),
        share(slow, runs)
      ]
    };
  });
  return { key: 'modules', columns: ['weeklyChannels', 'outputs', 'errors', 'errorRate', 'slowRate'], rows };
}

function tierTable(r: AnalyticsReads): Table {
  const accounts = latest(r.day, 'accounts.total');
  const enabled = latest(r.day, 'accounts.bot_enabled');
  return {
    key: 'tiers',
    columns: ['accounts', 'botEnabled'],
    rows: ['free', 'paid', 'vip'].map((tier) => ({
      label: tier,
      values: [
        cellText(cellFor(accounts, 'accounts.total', (d) => d[0] === tier)),
        cellText(cellFor(enabled, 'accounts.bot_enabled', (d) => d[0] === tier))
      ]
    }))
  };
}

function cohortLines(r: AnalyticsReads): CohortLine[] {
  return cohorts(r.cohortWeeks, r.cohortDays).map((c) => ({
    week: c.week,
    size: cellText(c.size),
    cells: c.cells.map((cell) => (cell ? `${cellText(cell)} (${share(cell, c.size)})` : ''))
  }));
}

export function buildAnalytics(r: AnalyticsReads): AnalyticsView {
  const tables: Table[] = [
    tierTable(r),
    list(latest(r.day, 'accounts.paid_source'), { key: 'paidSource', metric: 'accounts.paid_source' }),
    list(r.day, { key: 'lifecycle', metric: 'lifecycle' }),
    list(r.day, { key: 'billing', metric: 'billing.events' }),
    list(r.day, { key: 'presets', metric: 'onboarding.preset' }),
    moduleTable(r),
    list(r.day, { key: 'commands', metric: 'command.uses', limit: 30 }),
    matrix(r.day, { key: 'toggles', metric: 'module.toggles', labelAt: 0, colAt: 1, cols: ['on', 'off'] }),
    list(r.day, { key: 'customCommands', metric: 'custom.commands' }),
    list(r.day, { key: 'discordInteractions', metric: 'discord.interactions' }),
    list(r.day, { key: 'discordEvents', metric: 'discord.events' }),
    list(r.day, { key: 'outgress', metric: 'outgress.failures' }),
    list(r.day, { key: 'pages', metric: 'web.views', index: 1, limit: 40 }),
    matrix(r.day, { key: 'actions', metric: 'web.actions', labelAt: 1, colAt: 2, cols: ['ok', 'invalid', 'failed', 'denied'] }),
    list(latest(r.day, 'accounts.locale'), { key: 'locales', metric: 'accounts.locale' }),
    list(latest(r.day, 'accounts.pages_hidden'), { key: 'pagesHidden', metric: 'accounts.pages_hidden' })
  ];
  const series: Series[] = [
    { key: 'dauSeries', values: daily(r.day, 'channels.active') },
    { key: 'messagesSeries', values: daily(r.day, 'chat.messages') },
    { key: 'answeredSeries', values: daily(r.day, 'chat.answered') },
    { key: 'liveSeries', values: daily(r.day, 'channels.live') }
  ];
  const empty = r.day.length + r.week.length + r.month.length === 0;
  return { tiles: tiles(r), series, tables, cohorts: cohortLines(r), empty };
}
