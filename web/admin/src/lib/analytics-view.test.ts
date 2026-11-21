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
const catalog = await Bun.file(new URL('../../../../internal/domain/analytics/catalog.go', import.meta.url)).text();
const importSource = catalog.match(/KindImport:\s*\{"([^"]+)"/)![1];

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
  ...snap('2026-10-11', 'accounts.bot_enabled', [['free', 30], ['paid', 10]]),
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

  it('reports the period as UTC dates', () => {
    expect(view.period).toEqual({ start: '2026-10-11', end: '2026-11-09' });
  });

  it.each([
    { days: 1, start: '2026-11-09', active: [120], signups: 2, delta: 'day' },
    { days: 7, start: '2026-11-03', active: [100, 100, 100, 100, 100, 100, 120], signups: 6, delta: 'window' }
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
      period: { start, end: '2026-11-09' },
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
      ['dau', '100', 'delta.day', '0%', 'flat'],
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

  it('reads active channel tiles from closed periods only', () => {
    const open = buildAnalytics(reads({
      week: [...week, row(END, 'channels.active', [], 5)],
      month: [...month, row('2026-11-01', 'channels.active', [], 200)]
    }));
    expect(open.kpis.slice(0, 4).map((k) => [k.key, k.value, k.delta.params?.delta])).toEqual([
      ['dau', '100', '0%'],
      ['wau', '300', '+20%'],
      ['mau', '500', '+25%'],
      ['stickiness', '20%', '-5.0 pts']
    ]);
  });

  it('averages a series over complete days since tracking began', () => {
    const wide = buildAnalytics(reads({ days: 90 }));
    expect([wide.engagement.active.values.length, wide.engagement.active.average]).toEqual([90, '100']);
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
    ['churn', view.growth.churn, ['bot_disabled:2:-', 'revoked:1:-', 'chat_banned:0:-', 'account_deleted:3:-']],
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
    expect(facts(view.growth.churnFacts)).toEqual({ netAccounts: '+27', netBot: '+25' });
  });

  it('fills the signup series for every day', () => {
    const { values, labels } = view.growth.signups;
    expect({ length: values.length, first: labels[0], spikes: [values[1], values[21]] }).toEqual({
      length: 30,
      first: '2026-10-11',
      spikes: [20, 10]
    });
  });

  it.each([
    ['active', view.engagement.active, { length: 30, last: 120, ticks: [1, 8, 15, 22, 29], average: '100', peak: '120' }],
    ['live', view.engagement.live, { length: 0, last: undefined, ticks: [], average: '-', peak: '-' }],
    ['messages', view.engagement.messages, { length: 30, last: 0, ticks: [1, 8, 15, 22, 29], average: '34', peak: '1,000' }],
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
      'bans, revoked access and deletions lose the account, bot toggles do not',
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
          [at('2026-11-03'), 4, 1],
          [at('2026-11-04'), 2, 2]
        ],
        totals: [6, 6, 0, 1]
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
      lost: 4,
      net: 26,
      lossDays: 2
    });
  });
});

describe('bot stops and inactive accounts', () => {
  const life = (day: string, dim: string, value: number): AnalyticsRow => row(day, 'lifecycle', [dim], value);
  const runtime = (day: string, pairs: [string, number][]) => snap(day, 'accounts.runtime', pairs);
  const flows = (over: Partial<AnalyticsReads>) => {
    const { stops, inactive, dormant } = buildAnalytics(reads({ days: 7, ...over })).growth;
    return { stops, inactive, dormant };
  };
  const point = (rec: Record<string, number>) => Object.values(rec).join(':');

  const LATER = '2026-11-21';
  const stopPoints = (from: string, values: Record<string, string>) =>
    daysIn({ from, to: LATER }).map((day) => `${Date.parse(day)}:${values[day] ?? '0:0'}`);

  it('counts stops against comebacks from the day comebacks were recorded, leaving dormancy out', () => {
    const { stops } = flows({
      end: LATER,
      counters: [
        life('2026-11-17', 'bot_disabled', 5),
        life('2026-11-18', 'revoked', 2),
        life('2026-11-18', 'dormant', 1),
        life('2026-11-19', 'unbanned', 1),
        life('2026-11-20', 'bot_enabled', 1),
        life('2026-11-20', 'woke', 1),
        life('2026-11-20', 'account_deleted', 1),
        life('2026-11-20', 'signup', 9)
      ]
    });
    expect({ points: stops.points.map(point), facts: facts(stops.facts) }).toEqual({
      points: stopPoints('2026-11-18', { '2026-11-18': '2:0', '2026-11-19': '0:1', '2026-11-20': '1:1' }),
      facts: { stopped: '3', restarted: '2', netStops: '-1' }
    });
  });

  it('starts the stops chart on the first tracked day', () => {
    const { stops } = flows({
      end: LATER,
      counters: [row('2026-11-19', 'chat.messages', [], 10), life('2026-11-20', 'revoked', 1)]
    });
    expect(stops.points.map(point)).toEqual(stopPoints('2026-11-19', { '2026-11-20': '1:0' }));
  });

  it('splits accounts without a running bot on days with a snapshot', () => {
    const { inactive } = flows({
      counters: [life('2026-11-05', 'account_deleted', 3)],
      levels: [
        ...snap('2026-11-04', 'accounts.inactive', [['banned', 4], ['revoked', 3], ['disabled', 2]]),
        ...snap(END, 'accounts.inactive', [['banned', 6], ['revoked', 2], ['disabled', 5], ['other', 1]]),
        ...snap(END, 'accounts.total', [['free', 100], ['paid', 16]])
      ]
    });
    expect({ points: inactive.points.map(point), facts: facts(inactive.facts) }).toEqual({
      points: [`${Date.parse('2026-11-04')}:4:3:2:0`, `${Date.parse(END)}:6:2:5:1`],
      facts: { inactiveNow: '14', inactiveShare: '12%', inactiveChange: '+5', deletedWindow: '3' }
    });
  });

  it('keeps dormant channels apart from inactive accounts', () => {
    const { dormant, inactive } = flows({
      counters: [life('2026-11-06', 'dormant', 4), life('2026-10-01', 'dormant', 9)],
      levels: [
        ...runtime('2026-11-04', [['active', 80], ['dormant', 2]]),
        ...runtime(END, [['active', 70], ['dormant', 6], ['blocked', 4]])
      ]
    });
    expect({ points: dormant.points.map(point), facts: facts(dormant.facts), inactive: inactive.points }).toEqual({
      points: [`${Date.parse('2026-11-04')}:2`, `${Date.parse(END)}:6`],
      facts: { dormantNow: '6', dormantShare: '7.5%', wentDormant: '4' },
      inactive: []
    });
  });

  it('leaves every chart empty without rows', () => {
    const { stops, inactive, dormant } = flows({ counters: [], levels: [] });
    expect([stops.points, inactive.points, dormant.points, facts(inactive.facts), facts(dormant.facts)]).toEqual([
      [], [], [], { inactiveNow: '-', inactiveShare: '-', inactiveChange: '-', deletedWindow: '0' }, { dormantNow: '-', dormantShare: '-', wentDormant: '0' }
    ]);
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
            ...Array(2).fill({ percent: '0%', count: '0', heat: 0 }),
            ...Array(4).fill(null)
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
      ['govee', '50', '42%', '40', '80%', '0', '1', '0', '+1'],
      ['spotify', '30', '25%', '20', '67%', '1,000', '6', '2', '+4'],
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
    ['integrations', view.features.integrations, ['spotify:20:17%', 'govee:5:4.2%', 'fetch_key:0:0%']],
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
  it.each([1, 7, 30, 90, 365])('uses only the last full week for audience sizes with a %s day selection', (days) => {
    const sizes = buildAnalytics(reads({
      days,
      week: [
        ...snap('2026-11-02', 'channels.size', [['v0_9', 0], ['v10_49', 4], ['v50_199', 5], ['v200_999', 15], ['v1kp', 1]]),
        row('2026-11-09', 'channels.size', ['v50_199'], 1000),
        row('2026-10-26', 'channels.size', ['v50_199'], 2000)
      ],
      month: [row('2026-10-01', 'channels.size', ['v50_199'], 3000)]
    })).audience.sizes;
    expect({ labels: lines(sizes), values: sizes.map((bar) => bar.value) }).toEqual({
      labels: ['v0_9:<5:-', 'v10_49:<5:-', 'v50_199:5:25%', 'v200_999:15:75%', 'v1kp:<5:-'],
      values: [0, 0, 5, 15, 0]
    });
  });

  it('masks missing and entirely suppressed audience buckets without shares', () => {
    const sizes = buildAnalytics(reads({ week: [row('2026-11-02', 'channels.size', ['v0_9'], 4)] })).audience.sizes;
    expect(lines(sizes)).toEqual(['v0_9:<5:-', 'v10_49:<5:-', 'v50_199:<5:-', 'v200_999:<5:-', 'v1kp:<5:-']);
  });

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

describe('closed calendar periods', () => {
  it('zero-fills yesterday and the last full week and month instead of reusing older activity', () => {
    const sparse = buildAnalytics(reads({
      end: '2026-11-16',
      counters: [row('2026-09-01', 'chat.messages', [], 1), row('2026-11-14', 'channels.active', [], 10)],
      week: [row('2026-11-02', 'channels.active', [], 12)],
      month: [row('2026-09-01', 'channels.active', [], 20)]
    }));
    expect(sparse.kpis.slice(0, 4).map((k) => [k.value, k.delta.params?.delta])).toEqual([
      ['0', '-100%'], ['0', '-100%'], ['0', '-100%'], ['-', undefined]
    ]);
  });

  it('computes last full week uses even without a distinct-command row', () => {
    const sparse = buildAnalytics(reads({
      end: '2026-11-16',
      counters: [row('2026-11-10', 'command.uses', ['hello'], 7)],
      week
    }));
    expect({
      builtIn: cells(sparse.commands.builtIn),
      live: facts(sparse.engagement.facts).liveWeek,
      custom: facts(sparse.commands.custom).customChannelsWeek,
      modules: sparse.features.modules.rows.map((r) => r.cells[2].text)
    }).toEqual({ builtIn: [['hello', '7', '7', '0', '-']], live: '0', custom: '0', modules: ['0', '0'] });
  });

  it.each([
    ['2027-01-01', '2026-12-31', '2026-12-21', '2026-12-01'],
    ['2027-03-01', '2027-02-28', '2027-02-22', '2027-02-01']
  ])('uses UTC and ISO boundaries at %s', (end, day, weekStart, monthStart) => {
    const result = buildAnalytics(reads({
      end,
      counters: [row(day, 'channels.active', [], 2)],
      week: [row(weekStart, 'channels.active', [], 3)],
      month: [row(monthStart, 'channels.active', [], 4)]
    }));
    expect(result.kpis.slice(0, 3).map((k) => k.value)).toEqual(['2', '3', '4']);
  });

  it.each([
    ['2026-10-01', '3.2%'],
    ['2026-10-16', '6.3%']
  ])('starts stickiness coverage at tracking on %s', (start, expected) => {
    const sparse = buildAnalytics(reads({
      counters: [row(start, 'chat.messages', [], 1), row('2026-10-31', 'channels.active', [], 1)],
      month: [row('2026-10-01', 'channels.active', [], 1)]
    }));
    expect(sparse.kpis.find((k) => k.key === 'stickiness')?.value).toBe(expected);
  });
});

describe('snapshot and population corrections', () => {
  it.each([
    { first: [30, 10, 2], last: [40, 15, 3], expected: '+16' },
    { first: [30, 10, 2], last: [28, 8, 2], expected: '-4' },
    { first: [30, 10, 2], last: [30, 10, 2], expected: '0' }
  ])('uses all tiers at window snapshots: %j', ({ first, last, expected }) => {
    const levels = [
      row('2026-10-01', 'accounts.bot_enabled', ['free'], 999),
      ...snap('2026-10-12', 'accounts.bot_enabled', first.map((n, i) => [['free', 'paid', 'vip'][i], n])),
      ...snap(END, 'accounts.bot_enabled', last.map((n, i) => [['free', 'paid', 'vip'][i], n])),
      row('2026-11-10', 'accounts.bot_enabled', ['free'], 999)
    ];
    expect(facts(buildAnalytics(reads({ levels })).growth.churnFacts).netBot).toBe(expected);
  });

  it.each([
    ['stored opt-in', [], 0, 80],
    ['default on', ['alerts'], 5, 95],
    ['stored on floor', ['alerts'], 30, 80],
    ['off rows exceed accounts', ['alerts'], 150, 80]
  ] as [string, string[], number, number][])('uses effective enabled accounts for %s', (_name, defaults, off, on) => {
    const result = buildAnalytics(reads({
      levels: [
        row(END, 'accounts.total', ['free'], 100),
        row(END, 'accounts.bot_enabled', ['free'], 10),
        row(END, 'modules.rows', ['alerts', 'on'], 80),
        row(END, 'modules.rows', ['alerts', 'off'], off),
        ...defaults.map((name) => row(END, 'modules.defaults', [name], 1)),
        row(END, 'integrations.connected', ['spotify'], 30)
      ],
      week: [row('2026-11-02', 'module.active', ['alerts'], 8)]
    }));
    expect({
      module: cells(result.features.modules)[0],
      connection: result.features.integrations[0].secondary
    }).toEqual({
      module: ['alerts', String(on), pct(on, 100), '8', pct(8, on), '0', '0', '0', '0'],
      connection: { key: 'admin.analytics.share.ofAccounts', params: { share: '30%' } }
    });
  });

  it('shows default-on modules without stored rows or usage', () => {
    const result = buildAnalytics(reads({
      counters: [], week: [],
      levels: [row(END, 'accounts.total', ['free'], 100), row(END, 'modules.defaults', ['alerts'], 1)]
    }));
    expect(cells(result.features.modules)).toEqual([['alerts', '100', '100%', '0', '0%', '0', '0', '0', '0']]);
  });
});

describe('cohort coverage corrections', () => {
  it('excludes a truncated leading signup week and incomplete current week', () => {
    const result = buildAnalytics(reads({
      end: '2026-11-18',
      cohortDays: [
        row('2026-10-14', 'lifecycle', ['signup'], 1),
        row('2026-10-19', 'lifecycle', ['signup'], 10),
        row('2026-11-17', 'lifecycle', ['signup'], 2)
      ],
      cohortWeeks: [
        row('2026-10-12', 'cohort.active', ['2026-W42'], 7),
        row('2026-10-19', 'cohort.active', ['2026-W43'], 8),
        row('2026-11-16', 'cohort.active', ['2026-W43'], 1)
      ]
    }));
    expect(result.retention.rows).toEqual([{
      week: '2026-W43', size: '10', cells: [
        { percent: '80%', count: '8', heat: 0.8 },
        ...Array(3).fill({ percent: '0%', count: '0', heat: 0 }),
        ...Array(4).fill(null)
      ]
    }]);
  });
});

describe('extended analytics blocks', () => {
  const extended = buildAnalytics(reads({
    counters: [
      row('2026-10-11', 'chat.messages', [], 1),
      row('2026-11-03', 'recipe.runs', ['command', 'ok'], 6),
      row('2026-11-03', 'recipe.runs', ['command', 'failed'], 2),
      row('2026-11-04', 'recipe.runs', ['command', 'partial'], 2),
      row('2026-11-04', 'recipe.runs', ['timer', 'skipped'], 3),
      row('2026-10-01', 'recipe.runs', ['command', 'failed'], 100),
      row('2026-11-03', 'outgress.dropped', ['chat', 'chat_dropped'], 3),
      row('2026-11-03', 'outgress.dropped', ['helix', 'auth'], 2),
      row('2026-11-04', 'outgress.dropped', ['helix', 'exhausted'], 1),
      row('2026-11-03', 'provider.calls', ['weather', 'ok'], 7),
      ...['denied', 'limited', 'upstream_error', 'timeout', 'bad_def', 'error'].map((outcome) =>
        row('2026-11-03', 'provider.calls', ['weather', outcome], 1)),
      row('2026-11-04', 'provider.calls', ['twitch', 'ok'], 10),
      ...snap('2026-11-03', 'songs.requests', [['queued', 8], ['not_found', 2]]),
      ...snap('2026-11-03', 'command.blocked', [['perm', 2], ['offline', 1], ['cooldown', 2]]),
      row('2026-11-03', 'command.uses', ['custom'], 15),
      ...snap('2026-11-03', 'onboarding.steps', [['viewed', 20], ['consent_missing', 2], ['save_failed', 1]]),
      row('2026-11-03', 'import.runs', [importSource, 'preview_ok'], 5),
      row('2026-11-03', 'import.runs', [importSource, 'preview_failed'], 1),
      row('2026-11-04', 'import.runs', [importSource, 'committed'], 3),
      row('2026-11-04', 'import.runs', [importSource, 'commit_failed'], 1),
      row('2026-11-03', 'signup.via', ['chatgpt', 'started'], 6),
      row('2026-11-04', 'signup.via', ['chatgpt', 'signup'], 2),
      row('2026-11-04', 'signup.via', ['google', 'started'], 3),
      row('2026-11-03', 'dashboard.views', ['commands', 'owner'], 8),
      row('2026-11-03', 'dashboard.views', ['commands', 'delegate'], 2),
      row('2026-11-04', 'dashboard.views', ['overview', 'delegate'], 3),
      row('2026-11-03', 'billing.checkout', ['monthly', 'redirected'], 10),
      row('2026-11-03', 'billing.checkout', ['monthly', 'blocked'], 2),
      row('2026-11-04', 'billing.checkout', ['gift', 'unavailable'], 1),
      row('2026-11-04', 'billing.checkout', ['single', 'rejected'], 3),
      row('2026-11-04', 'billing.events', ['activate'], 4)
    ],
    levels: [
      row(END, 'accounts.total', ['free'], 100),
      row('2026-10-12', 'accounts.onboarded', [], 10),
      row(END, 'accounts.onboarded', [], 80),
      row(END, 'accounts.delegating', [], 12),
      row(END, 'recipes.total', ['command', 'on'], 20),
      row(END, 'recipes.total', ['command', 'off'], 4),
      row(END, 'recipes.total', ['timer', 'on'], 7)
    ],
    week: [
      row('2026-10-26', 'recipe.channels', [], 100),
      row('2026-11-02', 'recipe.channels', [], 8),
      ...snap('2026-11-02', 'activation.age', [['d0', 2], ['d1', 3], ['d2_6', 4], ['d7_13', 5], ['d14_29', 6], ['d30p', 7]]),
      row(END, 'activation.age', ['d0'], 99)
    ]
  }));

  it('groups recipe runs and outcomes by trigger and reads enabled recipe levels', () => {
    expect({
      runs: lines(extended.recipes.runs).slice(0, 2),
      outcomes: cells(extended.recipes.outcomes).slice(0, 1),
      totals: cells(extended.recipes.totals).filter((r) => ['command', 'timer'].includes(r[0])),
      channels: [extended.recipes.channels.value, extended.recipes.channels.delta.params?.delta]
    }).toEqual({
      runs: ['command:10:-', 'timer:3:-'],
      outcomes: [['command', '10', '6', '20%']],
      totals: [['command', '20', '4'], ['timer', '7', '0']],
      channels: ['8', '-92%']
    });
  });

  it('totals dropped sends by kind and reason and by day', () => {
    const drops = extended.reliability.dailyDrops;
    expect({ rows: cells(extended.reliability.drops), series: drops.values.slice(23, 26), length: drops.values.length }).toEqual({
      rows: [['chat', '0', '0', '0', '3'], ['helix', '2', '0', '1', '0']],
      series: [5, 1, 0], length: 30
    });
  });

  it('counts every non-ok provider outcome as an error', () => {
    expect(cells(extended.reliability.providers)).toEqual([['weather', '13', '46%'], ['twitch', '10', '0%']]);
  });

  it('shares song outcomes and command blocks against their own populations', () => {
    expect({
      songs: lines(extended.reliability.songs).slice(0, 2),
      success: facts(extended.reliability.songFacts),
      blocked: lines(extended.reliability.blocked)
    }).toEqual({
      songs: ['queued:8:80%', 'not_found:2:20%'],
      success: { songSuccess: '80%' },
      blocked: ['cooldown:2:10%', 'perm:2:10%', 'offline:1:5.0%']
    });
  });

  it('shows last full week activation ages, setup steps, imports, signup sources and onboarded account share', () => {
    expect({
      age: lines(extended.growth.activationAge),
      steps: lines(extended.growth.onboarding),
      imports: cells(extended.growth.imports),
      sources: cells(extended.growth.signupSources),
      onboarded: facts(extended.growth.onboarded)
    }).toEqual({
      sources: [['chatgpt', '6', '2'], ['google', '3', '0']],
      age: ['d0:2:-', 'd1:3:-', 'd2_6:4:-', 'd7_13:5:-', 'd14_29:6:-', 'd30p:7:-'],
      steps: ['viewed:20:-', 'consent_missing:2:-', 'save_failed:1:-'],
      imports: [[importSource, '5', '1', '3', '1']],
      onboarded: { onboarded: '80', onboardedShare: '80%' }
    });
  });

  it('splits dashboard section views by owner and delegate', () => {
    expect(extended.features.dashboard.slice(0, 2).map((b) => [b.id, b.value, b.secondary?.params])).toEqual([
      ['commands', 10, { owner: '8', delegate: '2' }], ['overview', 3, { owner: '0', delegate: '3' }]
    ]);
  });

  it('lists checkout results by plan and compares redirects with activation events', () => {
    expect({ rows: cells(extended.revenue.checkout), facts: facts(extended.revenue.checkoutFacts) }).toEqual({
      rows: [['monthly', '10', '2', '0', '0'], ['single', '0', '0', '0', '3'], ['gift', '0', '0', '1', '0']],
      facts: { redirected: '10', activated: '4', checkoutActivation: '40%' }
    });
  });

  it('shares accounts with delegates against all accounts', () => {
    expect(facts(extended.audience.delegating)).toEqual({ delegating: '12', delegatingShare: '12%' });
  });

  it('returns zero for missing covered weekly extensions and no share without a denominator', () => {
    const missing = buildAnalytics(reads({ counters: [], levels: [], week: [] }));
    expect({
      channels: missing.recipes.channels.value, age: lines(missing.growth.activationAge),
      onboarded: facts(missing.growth.onboarded), delegating: facts(missing.audience.delegating),
      songs: facts(missing.reliability.songFacts), checkout: facts(missing.revenue.checkoutFacts)
    }).toEqual({
      channels: '0', age: ['d0:0:-', 'd1:0:-', 'd2_6:0:-', 'd7_13:0:-', 'd14_29:0:-', 'd30p:0:-'],
      onboarded: { onboarded: '0', onboardedShare: '-' }, delegating: { delegating: '0', delegatingShare: '-' },
      songs: { songSuccess: '-' }, checkout: { redirected: '0', activated: '0', checkoutActivation: '-' }
    });
  });
});
