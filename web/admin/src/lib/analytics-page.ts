// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  addDays,
  bounds,
  byDim,
  cohorts,
  dailySeries,
  dateLabel,
  daysIn,
  decimal,
  fmt,
  inRange,
  latest,
  monthRange,
  pct,
  pctText,
  pick,
  pointsChange,
  previousStart,
  ratio,
  relativeChange,
  signed,
  snapshots,
  sum,
  total,
  valueAt,
  windows,
  withDim,
  type AnalyticsRow,
  type Change,
  type CohortRow,
  type DateRange,
  type Unit
} from './analytics-view';

export const ANALYTICS_WINDOWS = [30, 90, 365] as const;

export type AnalyticsReads = {
  end: string;
  days: number;
  counters: AnalyticsRow[];
  levels: AnalyticsRow[];
  week: AnalyticsRow[];
  month: AnalyticsRow[];
  cohortWeeks: AnalyticsRow[];
  cohortDays: AnalyticsRow[];
};

export type Phrase = { key: string; params?: Record<string, string> };
export type Name = Phrase | { text: string };
export type Trend = 'up' | 'down' | 'flat';

export type Kpi = { key: string; value: string; unit: Phrase | null; delta: Phrase; trend: Trend };
export type Bar = { id: string; name: Name; value: number; valueText: string; secondary: Phrase | null };
export type Fact = { key: string; value: string };
export type Series = { values: number[]; labels: string[]; ticks: number[]; average: string; peak: string };
export type TableCell = { text: string };
export type TableRow = { id: string; name: Name; cells: TableCell[] };
export type DataTable = { columns: string[]; rows: TableRow[] };
export type CohortCell = { percent: string; count: string; heat: number };
export type CohortLine = { week: string; size: string; cells: (CohortCell | null)[] };

export type AnalyticsView = {
  empty: boolean;
  period: { start: string; end: string };
  kpis: Kpi[];
  growth: { signups: Series; funnel: Bar[]; churn: Bar[]; churnFacts: Fact[]; presets: Bar[] };
  engagement: { active: Series; live: Series; messages: Series; answered: Series; facts: Fact[] };
  retention: { horizon: number; rows: CohortLine[] };
  revenue: { plans: DataTable; sources: Bar[]; billing: Bar[]; billingFacts: Fact[]; conversion: DataTable };
  features: { modules: DataTable; breadth: DataTable; breadthFacts: Fact[]; integrations: Bar[]; pagesHidden: Bar[] };
  commands: { builtIn: DataTable; custom: Fact[]; perms: Bar[]; flags: Bar[] };
  audience: { locales: Bar[] };
};

const ENUMS = {
  lifecycle: ['signup', 'onboarded', 'bot_enabled', 'bot_disabled', 'revoked', 'chat_banned', 'account_deleted'],
  billing: ['activate', 'cancel_requested', 'cancel_aborted', 'revoke', 'payment_failed', 'expired'],
  tier: ['free', 'paid', 'vip'],
  source: ['tebex', 'admin', 'giveaway', 'vip'],
  preset: ['start-new', 'integrate', 'quiet', 'import', 'songs'],
  link: ['spotify', 'govee', 'fetch_key'],
  page: ['commands', 'leaderboard', 'songqueue'],
  perm: ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'],
  flag: ['online_only', 'aliased', 'cooldown', 'counter']
} as const;

const FUNNEL = ['signup', 'onboarded', 'bot_enabled'] as const;
const CHURN = ['bot_disabled', 'revoked', 'account_deleted', 'chat_banned'] as const;
const HORIZON = 8;
const CONVERSION_WEEKS = 12;
const BUILT_IN_LIMIT = 30;
const PLAN_COLUMNS = ['accounts', 'accountShare', 'botEnabled', 'botEnabledShare'];
const BREADTH_COLUMNS = ['channels', 'share'];
const RETENTION_WEEKS = 12;
const CONVERSION_COLUMNS = ['accounts', 'paid', 'conversion'];
const MODULE_COLUMNS = ['on', 'adoption', 'activeWeek', 'activation', 'outputs', 'turnedOn', 'turnedOff', 'net'];
const BUILT_IN_COLUMNS = ['uses', 'usesWeek', 'channelsWeek', 'usesPerChannel'];
const UNIT_DELTA = { d: 'day', w: 'week', m: 'month' } as const;
const LANGUAGES = new Intl.DisplayNames(['en'], { type: 'language' });

type EnumName = keyof typeof ENUMS;
type ShareName = 'ofTotal' | 'ofAccounts' | 'ofBotEnabled' | 'ofCommands' | 'ofChannels';
type Entry = [string, number];
type Rank = [number, number];
type Ctx = { r: AnalyticsReads; days: number; cur: DateRange; prev: DateRange; prevComplete: boolean };
type DeltaSpec = { name: 'day' | 'week' | 'month' | 'since' | 'window'; days?: number };
type KpiSpec = { key: string; value: string; unit?: Phrase; change: Change; delta: DeltaSpec };
type BarSpec = {
  ids?: readonly string[];
  enum?: EnumName;
  order?: 'value' | 'numeric' | 'keep';
  limit?: number;
  name?: (id: string) => Name;
  secondary?: (value: number, index: number) => Phrase | null;
};
type ModuleStats = { on: Map<string, number>; active: Map<string, number>; outputs: Map<string, number>; up: Map<string, number>; down: Map<string, number> };

const byValue = ([a, x]: Entry, [b, y]: Entry): number => y - x || a.localeCompare(b);
const SORTS = {
  value: byValue,
  numeric: ([a]: Entry, [b]: Entry): number => Number(a) - Number(b),
  keep: (): number => 0
};

function context(r: AnalyticsReads): Ctx {
  const { cur, prev } = windows(r.end, r.days);
  const first = bounds(r.counters)?.first;
  return { r, days: r.days, cur, prev, prevComplete: first !== undefined && first <= prev.from };
}

const counter = (c: Ctx, metric: string): AnalyticsRow[] => inRange(pick(c.r.counters, metric), c.cur);
const level = (c: Ctx, metric: string): AnalyticsRow[] => latest(pick(c.r.levels, metric));
const weekly = (c: Ctx, metric: string): AnalyticsRow[] => latest(pick(c.r.week, metric));
const get = (values: Map<string, number>, id: string): number => values.get(id) ?? 0;

function facts(values: Record<string, string>): Fact[] {
  return Object.entries(values).map(([key, value]) => ({ key, value }));
}

function row(id: string, name: Name, texts: string[]): TableRow {
  return { id, name, cells: texts.map((text) => ({ text })) };
}

function enumName(id: string, group?: EnumName): Name {
  const known = group !== undefined && (ENUMS[group] as readonly string[]).includes(id);
  return known ? { key: `admin.analytics.dim.${group}.${id}` } : { text: id };
}

function ofShare(name: ShareName, whole: number): (value: number) => Phrase {
  return (value) => ({ key: `admin.analytics.share.${name}`, params: { share: pct(value, whole) } });
}

function bars(totals: Map<string, number>, spec: BarSpec): Bar[] {
  const base = spec.ids ? new Map(spec.ids.map((id): Entry => [id, get(totals, id)])) : totals;
  return [...base]
    .sort(SORTS[spec.order ?? 'value'])
    .slice(0, spec.limit)
    .map(([id, value], index) => ({
      id,
      name: spec.name?.(id) ?? enumName(id, spec.enum),
      value,
      valueText: fmt(value),
      secondary: spec.secondary?.(value, index) ?? null
    }));
}

function deltaPhrase(spec: DeltaSpec, change: Change): Phrase {
  if (!change) return { key: 'admin.analytics.delta.none' };
  const params: Record<string, string> = { delta: change.text };
  if (spec.days !== undefined) params.days = String(spec.days);
  return { key: `admin.analytics.delta.${spec.name}`, params };
}

function kpi(spec: KpiSpec): Kpi {
  return {
    key: spec.key,
    value: spec.value,
    unit: spec.unit ?? null,
    delta: deltaPhrase(spec.delta, spec.change),
    trend: spec.change?.trend ?? 'flat'
  };
}

function activeKpi(key: string, rows: AnalyticsRow[], unit: Unit): Kpi {
  const edge = bounds(rows);
  const now = edge ? valueAt(rows, edge.last) : 0;
  const before = edge ? valueAt(rows, previousStart(edge.last, unit)) : 0;
  return kpi({ key, value: fmt(now), change: relativeChange(now, before), delta: { name: UNIT_DELTA[unit] } });
}

function stickiness(c: Ctx, month: string): number | null {
  const daily = pick(c.r.counters, 'channels.active');
  const first = bounds(daily)?.first ?? month;
  const full = monthRange(month);
  const covered: DateRange = { from: first > full.from ? first : full.from, to: full.to };
  const length = daysIn(covered).length;
  const active = valueAt(pick(c.r.month, 'channels.active'), month);
  return ratio(sum(inRange(daily, covered)), length * active);
}

function stickinessKpi(c: Ctx): Kpi {
  const last = bounds(pick(c.r.month, 'channels.active'))?.last;
  const now = last ? stickiness(c, last) : null;
  const before = last ? stickiness(c, previousStart(last, 'm')) : null;
  return kpi({
    key: 'stickiness',
    value: now === null ? '-' : pctText(now),
    change: pointsChange(now, before),
    delta: { name: 'month' }
  });
}

function levelKpis(c: Ctx): Kpi[] {
  const accounts = snapshots(pick(c.r.levels, 'accounts.total'));
  const bot = snapshots(pick(c.r.levels, 'accounts.bot_enabled'));
  const [now, before] = [sum(accounts.last), sum(accounts.first)];
  const [botNow, botBefore] = [sum(bot.last), sum(bot.first)];
  const [paidNow, paidBefore] = [sum(withDim(accounts.last, 'paid')), sum(withDim(accounts.first, 'paid'))];
  const delta: DeltaSpec = { name: 'since', days: accounts.days };
  const gate = (change: Change): Change => (accounts.days > 0 ? change : null);
  const share = { key: 'admin.analytics.kpi.paidShare', params: { share: pct(paidNow, now) } };
  return [
    kpi({ key: 'accounts', value: fmt(now), change: gate(relativeChange(now, before)), delta }),
    kpi({
      key: 'botShare',
      value: pct(botNow, now),
      change: gate(pointsChange(ratio(botNow, now), ratio(botBefore, before))),
      delta
    }),
    kpi({ key: 'paid', value: fmt(paidNow), unit: share, change: gate(relativeChange(paidNow, paidBefore)), delta })
  ];
}

function signupsKpi(c: Ctx): Kpi {
  const rows = withDim(pick(c.r.counters, 'lifecycle'), 'signup');
  const now = sum(inRange(rows, c.cur));
  const before = sum(inRange(rows, c.prev));
  return kpi({
    key: 'signups',
    value: fmt(now),
    change: c.prevComplete ? relativeChange(now, before) : null,
    delta: { name: 'window', days: c.days }
  });
}

function kpis(c: Ctx): Kpi[] {
  return [
    activeKpi('dau', pick(c.r.counters, 'channels.active'), 'd'),
    activeKpi('wau', pick(c.r.week, 'channels.active'), 'w'),
    activeKpi('mau', pick(c.r.month, 'channels.active'), 'm'),
    stickinessKpi(c),
    ...levelKpis(c),
    signupsKpi(c)
  ];
}

function stepOf(values: number[]): (value: number, index: number) => Phrase | null {
  return (value, index) =>
    index === 0
      ? null
      : {
          key: 'admin.analytics.funnel.step',
          params: { ofSignups: pct(value, values[0]), ofPrevious: pct(value, values[index - 1]) }
        };
}

function growth(c: Ctx): AnalyticsView['growth'] {
  const life = byDim(counter(c, 'lifecycle'));
  const n = (id: string) => get(life, id);
  const presets = byDim(counter(c, 'onboarding.preset'));
  return {
    signups: dailySeries(withDim(pick(c.r.counters, 'lifecycle'), 'signup'), c.cur),
    funnel: bars(life, { ids: FUNNEL, enum: 'lifecycle', order: 'keep', secondary: stepOf(FUNNEL.map(n)) }),
    churn: bars(life, { ids: CHURN, enum: 'lifecycle', order: 'keep' }),
    churnFacts: facts({
      netAccounts: signed(n('signup') - n('account_deleted')),
      netBot: signed(n('bot_enabled') - n('bot_disabled') - n('revoked'))
    }),
    presets: bars(presets, { enum: 'preset', secondary: ofShare('ofTotal', total(presets)) })
  };
}

function engagement(c: Ctx): AnalyticsView['engagement'] {
  const series = (metric: string) => dailySeries(pick(c.r.counters, metric), c.cur);
  const count = (metric: string) => sum(counter(c, metric));
  const messages = count('chat.messages');
  const answered = count('chat.answered');
  return {
    active: series('channels.active'),
    live: series('channels.live'),
    messages: series('chat.messages'),
    answered: series('chat.answered'),
    facts: facts({
      messages: fmt(messages),
      answered: fmt(answered),
      answerRate: pct(answered, messages),
      liveStarts: fmt(count('live.starts')),
      timerFires: fmt(count('timer.fires')),
      liveWeek: fmt(sum(weekly(c, 'channels.live')))
    })
  };
}

function cohortLine(line: CohortRow): CohortLine {
  const cell = (active: number | null) =>
    active === null
      ? null
      : { percent: pct(active, line.size), count: fmt(active), heat: Math.min(1, Math.max(0, active / line.size)) };
  return { week: line.week, size: fmt(line.size), cells: line.cells.map(cell) };
}

function retention(c: Ctx): AnalyticsView['retention'] {
  const lines = cohorts(c.r.cohortWeeks, c.r.cohortDays, HORIZON).slice(0, RETENTION_WEEKS);
  return { horizon: HORIZON, rows: lines.map(cohortLine) };
}

function plans(c: Ctx): DataTable {
  const accounts = byDim(level(c, 'accounts.total'));
  const bot = byDim(level(c, 'accounts.bot_enabled'));
  const all = total(accounts);
  const rows = ENUMS.tier.map((id) => {
    const [a, b] = [get(accounts, id), get(bot, id)];
    return row(id, enumName(id, 'tier'), [fmt(a), pct(a, all), fmt(b), pct(b, a)]);
  });
  return { columns: PLAN_COLUMNS, rows };
}

function conversion(c: Ctx): DataTable {
  const accounts = byDim(level(c, 'accounts.by_signup_week'));
  const paid = byDim(level(c, 'accounts.paid_by_signup_week'));
  const weeks = [...accounts.keys()].sort().reverse().slice(0, CONVERSION_WEEKS);
  const rows = weeks.map((week) => {
    const [a, p] = [get(accounts, week), get(paid, week)];
    return row(week, { text: week }, [fmt(a), fmt(p), pct(p, a)]);
  });
  return { columns: CONVERSION_COLUMNS, rows };
}

function revenue(c: Ctx): AnalyticsView['revenue'] {
  const sources = byDim(level(c, 'accounts.paid_source'));
  return {
    plans: plans(c),
    sources: bars(sources, { enum: 'source', secondary: ofShare('ofTotal', total(sources)) }),
    billing: bars(byDim(counter(c, 'billing.events')), { ids: ENUMS.billing, enum: 'billing' }),
    billingFacts: facts({
      cancelPending: fmt(sum(level(c, 'accounts.cancel_pending'))),
      paymentFailed: fmt(sum(level(c, 'accounts.payment_failed')))
    }),
    conversion: conversion(c)
  };
}

function moduleLine(name: string, stats: ModuleStats, bot: number): TableRow {
  const [on, active, up, down] = [get(stats.on, name), get(stats.active, name), get(stats.up, name), get(stats.down, name)];
  const texts = [fmt(on), pct(on, bot), fmt(active), pct(active, on), fmt(get(stats.outputs, name)), fmt(up), fmt(down), signed(up - down)];
  return row(name, { text: name }, texts);
}

function moduleTable(c: Ctx): DataTable {
  const switches = level(c, 'modules.rows');
  const toggles = counter(c, 'module.toggles');
  const stats: ModuleStats = {
    on: byDim(withDim(switches, 'on', 1)),
    active: byDim(weekly(c, 'module.active')),
    outputs: byDim(counter(c, 'module.events')),
    up: byDim(withDim(toggles, 'on', 1)),
    down: byDim(withDim(toggles, 'off', 1))
  };
  const names = new Set([...byDim(switches).keys(), ...stats.active.keys(), ...stats.outputs.keys(), ...byDim(toggles).keys()]);
  const bot = sum(level(c, 'accounts.bot_enabled'));
  const ordered = [...names].sort((a, b) => get(stats.on, b) - get(stats.on, a) || a.localeCompare(b));
  return { columns: MODULE_COLUMNS, rows: ordered.map((name) => moduleLine(name, stats, bot)) };
}

function valueAtRank(counts: Rank[], rank: number): number {
  let seen = 0;
  for (const [modules, channels] of counts) {
    seen += channels;
    if (seen >= rank) return modules;
  }
  return 0;
}

function breadthFacts(breadth: Map<string, number>): Fact[] {
  const counts = [...breadth].map(([id, n]): Rank => [Number(id), n]).sort((a, b) => a[0] - b[0]);
  const channels = total(breadth);
  if (channels === 0) return facts({ avgModules: '-', medianModules: '-' });
  const weighted = counts.reduce((acc, [modules, n]) => acc + modules * n, 0);
  const median = (valueAtRank(counts, Math.ceil(channels / 2)) + valueAtRank(counts, Math.floor(channels / 2) + 1)) / 2;
  return facts({ avgModules: decimal(weighted / channels), medianModules: String(median) });
}

function breadthTable(breadth: Map<string, number>): DataTable {
  const whole = total(breadth);
  const rows = [...breadth]
    .sort(SORTS.numeric)
    .map(([dim, channels]) => row(dim, { text: dim }, [fmt(channels), pct(channels, whole)]));
  return { columns: BREADTH_COLUMNS, rows };
}

function features(c: Ctx): AnalyticsView['features'] {
  const breadth = byDim(level(c, 'modules.breadth'));
  const bot = sum(level(c, 'accounts.bot_enabled'));
  const accounts = sum(level(c, 'accounts.total'));
  return {
    modules: moduleTable(c),
    breadth: breadthTable(breadth),
    breadthFacts: breadthFacts(breadth),
    integrations: bars(byDim(level(c, 'integrations.connected')), {
      ids: ENUMS.link,
      enum: 'link',
      secondary: ofShare('ofBotEnabled', bot)
    }),
    pagesHidden: bars(byDim(level(c, 'accounts.pages_hidden')), {
      ids: ENUMS.page,
      enum: 'page',
      secondary: ofShare('ofAccounts', accounts)
    })
  };
}

function builtIn(c: Ctx): DataTable {
  const uses = pick(c.r.counters, 'command.uses');
  const window = byDim(inRange(uses, c.cur));
  window.delete('custom');
  const edge = bounds(pick(c.r.week, 'command.channels'));
  const week = edge ? byDim(inRange(uses, { from: edge.last, to: addDays(edge.last, 6) })) : new Map<string, number>();
  const channels = byDim(weekly(c, 'command.channels'));
  const rows = [...window]
    .sort(byValue)
    .slice(0, BUILT_IN_LIMIT)
    .map(([name, n]) => {
      const [used, reach] = [get(week, name), get(channels, name)];
      return row(name, { text: name }, [fmt(n), fmt(used), fmt(reach), reach === 0 ? '-' : decimal(used / reach)]);
    });
  return { columns: BUILT_IN_COLUMNS, rows };
}

function customFacts(c: Ctx): Fact[] {
  const library = byDim(counter(c, 'custom.commands'));
  const state = byDim(level(c, 'custom.total'));
  const [created, deleted] = [get(library, 'created'), get(library, 'deleted')];
  return facts({
    customCreated: fmt(created),
    customDeleted: fmt(deleted),
    customNet: signed(created - deleted),
    customUses: fmt(sum(withDim(counter(c, 'command.uses'), 'custom'))),
    customChannelsWeek: fmt(sum(withDim(weekly(c, 'command.channels'), 'custom'))),
    customChannels: fmt(sum(level(c, 'custom.channels'))),
    customActive: fmt(get(state, 'active')),
    customInactive: fmt(get(state, 'inactive'))
  });
}

function commands(c: Ctx): AnalyticsView['commands'] {
  const state = byDim(level(c, 'custom.total'));
  const secondary = ofShare('ofCommands', total(state));
  return {
    builtIn: builtIn(c),
    custom: customFacts(c),
    perms: bars(byDim(level(c, 'custom.perm')), { ids: ENUMS.perm, enum: 'perm', secondary }),
    flags: bars(byDim(level(c, 'custom.flags')), { ids: ENUMS.flag, enum: 'flag', secondary })
  };
}

function localeName(code: string): Name {
  try {
    const label = LANGUAGES.of(code);
    return { text: label && label !== code ? `${label} (${code})` : code };
  } catch {
    return { text: code };
  }
}

function audience(c: Ctx): AnalyticsView['audience'] {
  const accounts = sum(level(c, 'accounts.total'));
  const locales = byDim(level(c, 'accounts.locale'));
  return { locales: bars(locales, { name: localeName, secondary: ofShare('ofAccounts', accounts) }) };
}

function isEmpty(r: AnalyticsReads): boolean {
  return [r.counters, r.levels, r.week, r.month, r.cohortWeeks, r.cohortDays].every((rows) => rows.length === 0);
}

export function buildAnalytics(r: AnalyticsReads): AnalyticsView {
  const c = context(r);
  return {
    empty: isEmpty(r),
    period: { start: dateLabel(c.cur.from), end: dateLabel(c.cur.to) },
    kpis: kpis(c),
    growth: growth(c),
    engagement: engagement(c),
    retention: retention(c),
    revenue: revenue(c),
    features: features(c),
    commands: commands(c),
    audience: audience(c)
  };
}
