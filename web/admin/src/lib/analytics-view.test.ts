// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { buildAnalytics, gainLoss, type AnalyticsReads, type AnalyticsView, type Bar, type DataTable } from './analytics-page';
import {
  addDays,
  daysIn,
  fmt,
  isoWeek,
  currentDay,
  pct,
  pointsChange,
  relativeChange,
  signed,
  weekStart,
  windows,
  dateLabel,
  type AnalyticsRow,
  type Change
} from './analytics-view';

const END = '2026-11-09';
const THEN = '2026-10-10';

const row = (start: string, metric: string, dims: string[], value: number): AnalyticsRow => ({
  start,
  metric,
  dims,
  value
});

const snap = (day: string, metric: string, pairs: [string, number][]): AnalyticsRow[] =>
  pairs.map(([dim, value]) => row(day, metric, [dim], value));

const counters: AnalyticsRow[] = [
  ...daysIn({ from: '2026-09-11', to: END }).map((d) => row(d, 'channels.active', [], d === END ? 120 : 100)),
  row('2026-09-20', 'lifecycle', ['signup'], 15),
  row('2026-10-12', 'lifecycle', ['signup'], 20),
  row('2026-11-01', 'lifecycle', ['signup'], 10),
  row('2026-10-15', 'lifecycle', ['onboarded'], 12),
  row('2026-10-16', 'lifecycle', ['bot_enabled'], 8),
  row('2026-10-17', 'lifecycle', ['bot_disabled'], 2),
  row('2026-10-18', 'lifecycle', ['revoked'], 1),
  row('2026-10-19', 'lifecycle', ['account_deleted'], 3),
  row('2026-10-20', 'chat.messages', [], 1000),
  row('2026-10-20', 'chat.answered', [], 250),
  row('2026-10-20', 'live.starts', [], 7),
  row('2026-10-20', 'timer.fires', [], 3),
  ...snap('2026-10-21', 'onboarding.preset', [['start-new', 6], ['import', 2], ['mystery', 2]]),
  ...snap('2026-10-21', 'billing.events', [['activate', 5], ['cancel_requested', 2]]),
  row('2026-10-20', 'command.uses', ['!hello'], 100),
  row('2026-10-25', 'command.uses', ['!so'], 50),
  row('2026-10-20', 'command.uses', ['custom'], 999),
  row('2026-11-03', 'command.uses', ['!hello'], 40),
  row('2026-10-22', 'module.events', ['spotify'], 1000),
  row('2026-10-22', 'module.toggles', ['spotify', 'on'], 6),
  row('2026-10-22', 'module.toggles', ['spotify', 'off'], 2),
  row('2026-10-22', 'module.toggles', ['govee', 'on'], 1),
  row('2026-10-22', 'module.toggles', ['songs', 'on'], 3),
  ...snap('2026-10-23', 'custom.commands', [['created', 7], ['deleted', 2]])
];

const levels: AnalyticsRow[] = [
  ...snap(THEN, 'accounts.total', [['free', 80], ['paid', 15], ['vip', 5]]),
  ...snap(THEN, 'accounts.bot_enabled', [['free', 30], ['paid', 10]]),
  ...snap(END, 'accounts.total', [['free', 90], ['paid', 25], ['vip', 5]]),
  ...snap(END, 'accounts.bot_enabled', [['free', 40], ['paid', 20], ['vip', 5]]),
  ...snap(END, 'accounts.paid_source', [['tebex', 20], ['giveaway', 4], ['mystery', 1]]),
  ...snap(END, 'accounts.cancel_pending', [['paid', 4]]),
  ...snap(END, 'accounts.payment_failed', [['paid', 1]]),
  ...snap(END, 'accounts.locale', [['en', 90], ['fr', 20], ['!!', 2]]),
  ...snap(END, 'accounts.pages_hidden', [['songqueue', 3], ['commands', 1]]),
  ...snap(END, 'accounts.by_signup_week', [['2026-W41', 40], ['2026-W42', 50]]),
  ...snap(END, 'accounts.paid_by_signup_week', [['2026-W41', 4]]),
  row(END, 'modules.rows', ['spotify', 'on'], 30),
  row(END, 'modules.rows', ['spotify', 'off'], 12),
  row(END, 'modules.rows', ['govee', 'on'], 50),
  row(END, 'modules.rows', ['govee', 'off'], 2),
  ...snap(END, 'modules.breadth', [['1', 5], ['2', 10], ['10', 5]]),
  ...snap(END, 'integrations.connected', [['spotify', 20], ['govee', 5]]),
  ...snap(END, 'custom.total', [['active', 80], ['inactive', 20]]),
  ...snap(END, 'custom.perm', [['everyone', 60], ['mod', 30]]),
  ...snap(END, 'custom.flags', [['cooldown', 25]]),
  row(END, 'custom.channels', [], 15)
];

const week: AnalyticsRow[] = [
  row('2026-11-02', 'channels.active', [], 300),
  row('2026-10-26', 'channels.active', [], 250),
  row('2026-11-02', 'channels.live', [], 40),
  row('2026-11-02', 'module.active', ['spotify'], 20),
  row('2026-11-02', 'module.active', ['govee'], 40),
  row('2026-11-02', 'command.channels', ['!hello'], 20),
  row('2026-11-02', 'command.channels', ['custom'], 12)
];

const month: AnalyticsRow[] = [
  row('2026-10-01', 'channels.active', [], 500),
  row('2026-09-01', 'channels.active', [], 400)
];

const cohortDays: AnalyticsRow[] = [
  row('2026-10-12', 'lifecycle', ['signup'], 12),
  row('2026-10-14', 'lifecycle', ['signup'], 8),
  row('2026-10-05', 'lifecycle', ['signup'], 0),
  row('2026-10-13', 'lifecycle', ['onboarded'], 9)
];

const cohortWeeks: AnalyticsRow[] = [
  row('2026-10-12', 'cohort.active', ['2026-W42'], 18),
  row('2026-10-19', 'cohort.active', ['2026-W42'], 10),
  row('2026-12-07', 'cohort.active', ['2026-W42'], 5)
];

const reads = (over: Partial<AnalyticsReads> = {}): AnalyticsReads => ({
  end: END,
  days: 30,
  counters,
  levels,
  week,
  month,
  cohortWeeks,
  cohortDays,
  ...over
});

const view: AnalyticsView = buildAnalytics(reads());

const lines = (bars: Bar[]): string[] =>
  bars.map((b) => `${b.id}:${b.valueText}:${Object.values(b.secondary?.params ?? {})[0] ?? '-'}`);

const cells = (table: DataTable): string[][] => table.rows.map((r) => [r.id, ...r.cells.map((c) => c.text)]);

const facts = (list: { key: string; value: string }[]): Record<string, string> =>
  Object.fromEntries(list.map((f) => [f.key, f.value]));

describe('formatting', () => {
  it.each([
    [0, '0'],
    [999, '999'],
    [1234567, '1,234,567']
  ])('fmt(%p) is %p', (n, text) => {
    expect(fmt(n)).toBe(text);
  });

  it.each([
    [12, '+12'],
    [-3, '-3'],
    [0, '0'],
    [1500, '+1,500']
  ])('signed(%p) is %p', (n, text) => {
    expect(signed(n)).toBe(text);
  });

  it.each([
    [0, 0, '-'],
    [0, 10, '0%'],
    [1, 100, '1.0%'],
    [41, 1000, '4.1%'],
    [1, 3, '33%'],
    [64, 100, '64%'],
    [996, 1000, '99.6%'],
    [9996, 10000, '99.9%'],
    [100, 100, '100%'],
    [150, 100, '150%']
  ])('pct(%p, %p) is %p', (part, whole, text) => {
    expect(pct(part, whole)).toBe(text);
  });

  it.each([
    [112, 100, { text: '+12%', trend: 'up' }],
    [97, 100, { text: '-3%', trend: 'down' }],
    [100, 100, { text: '0%', trend: 'flat' }],
    [5, 0, null]
  ] as [number, number, Change][])('relativeChange(%p, %p)', (current, previous, expected) => {
    expect(relativeChange(current, previous)).toEqual(expected);
  });

  it.each([
    [11.2, 10, { text: '+1.2 pts', trend: 'up' }],
    [9.6, 10, { text: '-0.4 pts', trend: 'down' }],
    [10, 10, { text: '0.0 pts', trend: 'flat' }],
    [null, 10, null],
    [10, null, null]
  ] as [number | null, number | null, Change][])('pointsChange(%p, %p)', (current, previous, expected) => {
    expect(pointsChange(current, previous)).toEqual(expected);
  });

  it.each([
    ['2026-11-09', 'Nov 9, 2026'],
    ['2027-01-01', 'Jan 1, 2027']
  ])('dateLabel(%p) is %p', (day, text) => {
    expect(dateLabel(day)).toBe(text);
  });

  it.each([
    ['2026-11-10T00:00:30Z', '2026-11-10'],
    ['2026-01-01T23:59:59Z', '2026-01-01']
  ])('currentDay(%p) is %p', (now, day) => {
    expect(currentDay(new Date(now))).toBe(day);
  });
});

describe('weeks', () => {
  it.each([
    ['2026-12-31', '2026-W53'],
    ['2027-01-04', '2027-W01']
  ])('isoWeek(%p) is %p', (day, label) => {
    expect(isoWeek(day)).toBe(label);
  });

  it.each([
    ['2026-W42', '2026-10-12'],
    ['2027-W01', '2027-01-04']
  ])('weekStart(%p) is %p', (label, day) => {
    expect(weekStart(label)).toBe(day);
  });
});

describe('windows', () => {
  it('splits the current and previous window', () => {
    expect(windows(END, 30)).toEqual({
      cur: { from: '2026-10-11', to: END },
      prev: { from: '2026-09-11', to: THEN }
    });
  });

  it('reports the period as formatted dates', () => {
    expect(view.period).toEqual({ start: 'Oct 11, 2026', end: 'Nov 9, 2026' });
  });

  it.each([
    { days: 1, start: 'Nov 9, 2026', active: [120], signups: 2, delta: 'day' },
    { days: 7, start: 'Nov 3, 2026', active: [100, 100, 100, 100, 100, 100, 120], signups: 6, delta: 'window' }
  ])('builds the closed-day window %j', ({ days, start, active, signups, delta }) => {
    const short = buildAnalytics(reads({
      days,
      counters: [
        ...counters,
        row('2026-11-02', 'lifecycle', ['signup'], 10),
        row('2026-11-03', 'lifecycle', ['signup'], 3),
        row('2026-11-08', 'lifecycle', ['signup'], 1),
        row('2026-11-09', 'lifecycle', ['signup'], 2),
        row('2026-11-10', 'lifecycle', ['signup'], 99)
      ]
    }));
    const signupKpi = short.kpis.find((k) => k.key === 'signups');
    expect({ period: short.period, active: short.engagement.active.values, signups: signupKpi?.value, delta: signupKpi?.delta.key }).toEqual({
      period: { start, end: 'Nov 9, 2026' },
      active: [...active],
      signups: String(signups),
      delta: `admin.analytics.delta.${delta}`
    });
  });
});

describe('kpis', () => {
  it('computes every headline number with its delta', () => {
    const summary = view.kpis.map((k) => [
      k.key,
      k.value,
      k.delta.key.replace('admin.analytics.', ''),
      k.delta.params?.delta ?? '',
      k.trend
    ]);
    expect(summary).toEqual([
      ['dau', '120', 'delta.day', '+20%', 'up'],
      ['wau', '300', 'delta.week', '+20%', 'up'],
      ['mau', '500', 'delta.month', '+25%', 'up'],
      ['stickiness', '20%', 'delta.month', '-5.0 pts', 'down'],
      ['accounts', '120', 'delta.since', '+20%', 'up'],
      ['botShare', '54%', 'delta.since', '+14.2 pts', 'up'],
      ['paid', '25', 'delta.since', '+67%', 'up'],
      ['signups', '30', 'delta.window', '+100%', 'up']
    ]);
  });

  it.each([
    ['paid', { key: 'admin.analytics.kpi.paidShare', params: { share: '21%' } }, '30'],
    ['signups', null, '30'],
    ['dau', null, undefined]
  ])('%s carries its unit and day count', (key, unit, days) => {
    const found = view.kpis.find((k) => k.key === key);
    expect({ unit: found?.unit, days: found?.delta.params?.days }).toEqual({ unit, days });
  });

  it('drops the signups delta when the previous window is incomplete', () => {
    const wide = buildAnalytics(reads({ days: 365 }));
    expect(wide.kpis.find((k) => k.key === 'signups')).toEqual({
      key: 'signups',
      value: '45',
      unit: null,
      delta: { key: 'admin.analytics.delta.none' },
      trend: 'flat'
    });
  });

  it('shows no comparison for an empty read', () => {
    const empty = buildAnalytics(reads({ counters: [], levels: [], week: [], month: [], cohortWeeks: [], cohortDays: [] }));
    expect(empty.kpis.map((k) => [k.value, k.delta.key, k.trend])).toEqual([
      ['0', 'admin.analytics.delta.none', 'flat'],
      ['0', 'admin.analytics.delta.none', 'flat'],
      ['0', 'admin.analytics.delta.none', 'flat'],
      ['-', 'admin.analytics.delta.none', 'flat'],
      ['0', 'admin.analytics.delta.none', 'flat'],
      ['-', 'admin.analytics.delta.none', 'flat'],
      ['0', 'admin.analytics.delta.none', 'flat'],
      ['0', 'admin.analytics.delta.none', 'flat']
    ]);
    expect(empty.empty).toBe(true);
    expect(view.empty).toBe(false);
  });
});

describe('growth and engagement', () => {
  it.each([
    ['funnel', view.growth.funnel, ['signup:30:-', 'onboarded:12:40%', 'bot_enabled:8:27%']],
    ['churn', view.growth.churn, ['bot_disabled:2:-', 'revoked:1:-', 'account_deleted:3:-', 'chat_banned:0:-']],
    ['presets', view.growth.presets, ['start-new:6:60%', 'import:2:20%', 'mystery:2:20%']]
  ])('%s bars', (_name, bars, expected) => {
    expect(lines(bars)).toEqual(expected);
  });

  it('gives funnel steps both conversions', () => {
    expect(view.growth.funnel.map((b) => b.secondary)).toEqual([
      null,
      { key: 'admin.analytics.funnel.step', params: { ofSignups: '40%', ofPrevious: '40%' } },
      { key: 'admin.analytics.funnel.step', params: { ofSignups: '27%', ofPrevious: '67%' } }
    ]);
  });

  it('reports net account and bot movement', () => {
    expect(facts(view.growth.churnFacts)).toEqual({ netAccounts: '+27', netBot: '+5' });
  });

  it('fills the signup series for every day', () => {
    const { values, labels } = view.growth.signups;
    expect({ length: values.length, first: labels[0], spikes: [values[1], values[21]] }).toEqual({
      length: 30,
      first: 'Oct 11, 2026',
      spikes: [20, 10]
    });
  });

  it.each([
    ['active', view.engagement.active, { length: 30, last: 120, ticks: [1, 8, 15, 22, 29], average: '101', peak: '120' }],
    ['live', view.engagement.live, { length: 0, last: undefined, ticks: [], average: '-', peak: '-' }],
    ['messages', view.engagement.messages, { length: 30, last: 0, ticks: [1, 8, 15, 22, 29], average: '33', peak: '1,000' }],
    ['signups', view.growth.signups, { length: 30, last: 0, ticks: [1, 8, 15, 22, 29], average: '1.0', peak: '20' }]
  ])('%s series', (_name, series, expected) => {
    expect({
      length: series.values.length,
      last: series.values.at(-1),
      ticks: series.ticks,
      average: series.average,
      peak: series.peak
    }).toEqual(expected);
  });

  it('ticks the first of each month for a 365 day window', () => {
    const wide = buildAnalytics(reads({ days: 365 })).engagement.active;
    expect({ length: wide.values.length, ticks: wide.ticks.length, first: wide.ticks[0] }).toEqual({
      length: 365,
      ticks: 12,
      first: 21
    });
  });

  it('summarises engagement facts', () => {
    expect(facts(view.engagement.facts)).toEqual({
      messages: '1,000',
      answered: '250',
      answerRate: '25%',
      liveStarts: '7',
      timerFires: '3',
      liveWeek: '40'
    });
  });
});

describe('gained and lost accounts', () => {
  const range = { from: '2026-11-01', to: '2026-11-04' };
  const life = (day: string, dim: string, value: number): AnalyticsRow => row(day, 'lifecycle', [dim], value);
  const shape = (rows: AnalyticsRow[]) => {
    const result = gainLoss(rows, range);
    return {
      points: result.points.map((p) => [p.at, p.gained, p.lost]),
      totals: [result.gained, result.lost, result.net, result.lossDays]
    };
  };
  const at = (day: string) => Date.parse(day);

  it.each([
    ['empty window', [], { points: [], totals: [0, 0, 0, 0] }],
    [
      'only other lifecycle events',
      [life('2026-11-02', 'onboarded', 9), row('2026-11-02', 'chat.messages', [], 5)],
      { points: [], totals: [0, 0, 0, 0] }
    ],
    [
      'loss only, none, and mixed days',
      [
        life('2026-11-01', 'revoked', 3),
        life('2026-11-03', 'signup', 4),
        life('2026-11-03', 'bot_disabled', 1),
        life('2026-11-03', 'chat_banned', 1),
        life('2026-11-04', 'account_deleted', 2),
        life('2026-11-04', 'signup', 2),
        life('2026-10-31', 'signup', 50)
      ],
      {
        points: [
          [at('2026-11-01'), 0, 3],
          [at('2026-11-02'), 0, 0],
          [at('2026-11-03'), 4, 2],
          [at('2026-11-04'), 2, 2]
        ],
        totals: [6, 7, -1, 1]
      }
    ]
  ])('%s', (_name, rows, expected) => {
    expect(shape(rows)).toEqual(expected);
  });

  it('feeds the growth section from the window', () => {
    const { points, gained, lost, net, lossDays } = view.growth.gainLoss;
    expect({ length: points.length, gained, lost, net, lossDays }).toEqual({
      length: 30,
      gained: 30,
      lost: 6,
      net: 24,
      lossDays: 3
    });
  });
});

describe('retention', () => {
  it('places cohort cells by weeks since signup and sizes them from signups', () => {
    expect(view.retention).toEqual({
      horizon: 8,
      rows: [
        {
          week: '2026-W42',
          size: '20',
          cells: [
            { percent: '90%', count: '18', heat: 0.9 },
            { percent: '50%', count: '10', heat: 0.5 },
            ...Array(6).fill(null)
          ]
        }
      ]
    });
  });
});

describe('retention window', () => {
  it('keeps the 12 most recent signup weeks, newest first', () => {
    const signups = Array.from({ length: 14 }, (_, i) => row(addDays('2026-10-12', -7 * i), 'lifecycle', ['signup'], 5));
    const weeks = buildAnalytics(reads({ cohortDays: signups })).retention.rows.map((r) => r.week);
    expect({ count: weeks.length, first: weeks[0], last: weeks[11] }).toEqual({
      count: 12,
      first: '2026-W42',
      last: '2026-W31'
    });
  });
});

describe('revenue', () => {
  it('lists plans with their shares', () => {
    expect(cells(view.revenue.plans)).toEqual([
      ['free', '90', '75%', '40', '44%'],
      ['paid', '25', '21%', '20', '80%'],
      ['vip', '5', '4.2%', '5', '100%']
    ]);
  });

  it.each([
    ['sources', lines(view.revenue.sources), ['tebex:20:80%', 'giveaway:4:16%', 'mystery:1:4.0%']],
    [
      'billing',
      lines(view.revenue.billing),
      ['activate:5:-', 'cancel_requested:2:-', 'cancel_aborted:0:-', 'expired:0:-', 'payment_failed:0:-', 'revoke:0:-']
    ]
  ])('%s bars', (_name, actual, expected) => {
    expect(actual).toEqual(expected);
  });

  it('names unknown enum values as plain text', () => {
    expect(view.revenue.sources.map((b) => b.name)).toEqual([
      { key: 'admin.analytics.dim.source.tebex' },
      { key: 'admin.analytics.dim.source.giveaway' },
      { text: 'mystery' }
    ]);
  });

  it('reports pending cancellations and failed payments', () => {
    expect(facts(view.revenue.billingFacts)).toEqual({ cancelPending: '4', paymentFailed: '1' });
  });

  it('lists conversion by signup week, newest first', () => {
    expect(cells(view.revenue.conversion)).toEqual([
      ['2026-W42', '50', '0', '0%'],
      ['2026-W41', '40', '4', '10%']
    ]);
  });
});

describe('features', () => {
  it('builds the module table sorted by enabled channels', () => {
    expect(cells(view.features.modules)).toEqual([
      ['govee', '50', '77%', '40', '80%', '0', '1', '0', '+1'],
      ['spotify', '30', '46%', '20', '67%', '1,000', '6', '2', '+4'],
      ['songs', '0', '0%', '0', '-', '0', '3', '0', '+3']
    ]);
  });

  it('keeps the module column order', () => {
    expect(view.features.modules.columns).toEqual([
      'on',
      'adoption',
      'activeWeek',
      'activation',
      'outputs',
      'turnedOn',
      'turnedOff',
      'net'
    ]);
  });

  it('sorts breadth numerically with channel counts and shares', () => {
    expect({ columns: view.features.breadth.columns, rows: cells(view.features.breadth) }).toEqual({
      columns: ['channels', 'share'],
      rows: [
        ['1', '5', '25%'],
        ['2', '10', '50%'],
        ['10', '5', '25%']
      ]
    });
  });

  it.each([
    [[['10', 5], ['2', 10], ['1', 5]], ['1', '2', '10'], '3.8', '2'],
    [[['1', 5], ['3', 5]], ['1', '3'], '2.0', '2'],
    [[['1', 5], ['2', 5]], ['1', '2'], '1.5', '1.5'],
    [[], [], '-', '-']
  ] as [[string, number][], string[], string, string][])('breadth %j', (counts, order, avg, median) => {
    const sparse = buildAnalytics(
      reads({ counters: [], levels: counts.map(([n, v]) => row(END, 'modules.breadth', [n], v)) })
    );
    expect({ order: sparse.features.breadth.rows.map((r) => r.id), facts: facts(sparse.features.breadthFacts) }).toEqual({
      order,
      facts: { avgModules: avg, medianModules: median }
    });
  });

  it.each([
    ['integrations', view.features.integrations, ['spotify:20:31%', 'govee:5:7.7%', 'fetch_key:0:0%']],
    ['pagesHidden', view.features.pagesHidden, ['songqueue:3:2.5%', 'commands:1:0.8%', 'leaderboard:0:0%']]
  ])('%s bars', (_name, bars, expected) => {
    expect(lines(bars)).toEqual(expected);
  });
});

describe('commands', () => {
  it('lists built-in commands without custom ones and computes uses per channel', () => {
    expect(cells(view.commands.builtIn)).toEqual([
      ['!hello', '140', '40', '20', '2.0'],
      ['!so', '50', '0', '0', '-']
    ]);
  });

  it('summarises the custom command library', () => {
    expect(facts(view.commands.custom)).toEqual({
      customCreated: '7',
      customDeleted: '2',
      customNet: '+5',
      customUses: '999',
      customChannelsWeek: '12',
      customChannels: '15',
      customActive: '80',
      customInactive: '20'
    });
  });

  it.each([
    [
      'perms',
      view.commands.perms,
      ['everyone:60:60%', 'mod:30:30%', 'broadcaster:0:0%', 'lead_mod:0:0%', 'sub:0:0%', 'vip:0:0%']
    ],
    ['flags', view.commands.flags, ['cooldown:25:25%', 'aliased:0:0%', 'counter:0:0%', 'online_only:0:0%']]
  ])('%s bars', (_name, bars, expected) => {
    expect(lines(bars)).toEqual(expected);
  });
});

describe('audience', () => {
  it('names locales in English and falls back to the code', () => {
    expect(view.audience.locales.map((b) => b.name)).toEqual([
      { text: 'English (en)' },
      { text: 'French (fr)' },
      { text: '!!' }
    ]);
  });

  it('shares locales of all accounts', () => {
    expect(lines(view.audience.locales)).toEqual(['en:90:75%', 'fr:20:17%', '!!:2:1.7%']);
  });
});
