// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import {
  addDays,
  bounds,
  byDay,
  byDim,
  cohorts,
  dailySeries,
  dateLabel,
  daysIn,
  decimal,
  fmt,
  inRange,
  latest,
  lastClosedStart,
  monthRange,
  pct,
  pctText,
  pick,
  pointsChange,
  previousStart,
  ratio,
  relativeChange,
  rowsAt,
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
  type Count,
  type DateRange,
  type Day,
  type Dim,
  type Metric,
  type Slot,
  type Unit
} from './analytics-view';

export const ANALYTICS_WINDOWS = [1, 7, 30, 90, 365] as const;
export const DEFAULT_ANALYTICS_WINDOW = 30;
export const ANALYTICS_LIVE = 'admin:analytics';
export const ANALYTICS_REFRESH_MS = 15_000;

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
export type GainLossPoint = { at: number; gained: number; lost: number };
export type GainLoss = { points: GainLossPoint[]; gained: number; lost: number; net: number; lossDays: number };
export type TableCell ={ text: string };
export type TableRow = { id: string; name: Name; cells: TableCell[] };
export type DataTable = { columns: string[]; rows: TableRow[] };
export type CohortCell = { percent: string; count: string; heat: number };
export type CohortLine = { week: string; size: string; cells: (CohortCell | null)[] };

export type AnalyticsView = {
  empty: boolean;
  period: { start: string; end: string };
  kpis: Kpi[];
  growth: { signups: Series; gainLoss: GainLoss; funnel: Bar[]; churn: Bar[]; churnFacts: Fact[]; presets: Bar[]; activationAge: Bar[]; onboarding: Bar[]; imports: DataTable; onboarded: Fact[]; login: Bar[]; trials: Fact[] };
  engagement: { active: Series; live: Series; messages: Series; answered: Series; facts: Fact[] };
  retention: { horizon: number; rows: CohortLine[] };
  revenue: { plans: DataTable; sources: Bar[]; billing: Bar[]; billingFacts: Fact[]; conversion: DataTable; checkout: DataTable; checkoutFacts: Fact[] };
  features: { modules: DataTable; breadth: DataTable; breadthFacts: Fact[]; integrations: Bar[]; pagesHidden: Bar[]; dashboard: Bar[] };
  commands: { builtIn: DataTable; custom: Fact[]; perms: Bar[]; flags: Bar[] };
  audience: { sizes: Bar[]; locales: Bar[]; delegating: Fact[]; runtime: Bar[]; desktop: Bar[] };
  community: { ticks: Bar[]; timers: Bar[]; rounds: DataTable; entrants: DataTable; points: Bar[]; rewards: DataTable };
  recipes: { runs: Bar[]; outcomes: DataTable; channels: Kpi; totals: DataTable };
  reliability: { drops: DataTable; dailyDrops: Series; providers: DataTable; songs: Bar[]; songFacts: Fact[]; blocked: Bar[]; flags: DataTable };
};

const ENUMS = {
  size: ['v0_9', 'v10_49', 'v50_199', 'v200_999', 'v1kp'],
  tick: ['fired', 'gated', 'stopped', 'floor', 'blank', 'refused'],
  opt: ['plain', 'gated', 'capped', 'ends', 'offline'],
  rule: ['ip_logger', 'scam', 'phish', 'heuristic', 'block_term', 'lex_hate', 'lex_harassment', 'lex_sexual', 'lex_profanity', 'council_campaign', 'repeat', 'campaign', 'shield_mode', 'other'],
  runtime: ['active', 'dormant', 'blocked', 'disabled', 'recovering', 'degraded'],
  game: ['raffle', 'mraffle', 'duel', 'gamble'],
  flow: ['gamble', 'duel_stake', 'transfer', 'adjust', 'reward_grant', 'raffle_payout'],
  feature: ['channelpoints', 'govee'],
  login: ['started', 'ok', 'declined', 'oauth', 'state', 'bot', 'scope', 'link', 'retry', 'banned'],
  platform: ['macos', 'windows', 'linux'],
  lifecycle: ['signup', 'onboarded', 'bot_enabled', 'bot_disabled', 'revoked', 'chat_banned', 'account_deleted'],
  billing: ['activate', 'cancel_requested', 'cancel_aborted', 'revoke', 'payment_failed', 'expired'],
  tier: ['free', 'paid', 'vip'],
  source: ['tebex', 'admin', 'giveaway', 'vip'],
  preset: ['start-new', 'integrate', 'quiet', 'import', 'songs'],
  link: ['spotify', 'govee', 'fetch_key'],
  page: ['commands', 'leaderboard', 'songqueue'],
  perm: ['everyone', 'sub', 'vip', 'mod', 'lead_mod', 'broadcaster'],
  flag: ['online_only', 'aliased', 'cooldown', 'counter'],
  trigger: ['command', 'event', 'timer', 'reward', 'manual', 'countdown'],
  age: ['d0', 'd1', 'd2_6', 'd7_13', 'd14_29', 'd30p'],
  block: ['perm', 'offline', 'cooldown'],
  song: ['queued', 'off', 'perm', 'offline', 'not_found', 'queue_full', 'player_refused'],
  send: ['chat', 'helix'],
  drop: ['auth', 'rejected', 'exhausted', 'chat_dropped'],
  plan: ['monthly', 'single', 'gift'],
  onboard: ['viewed', 'consent_missing', 'save_failed'],
  section: ['overview', 'commands', 'flows', 'automations', 'modules', 'timers', 'loyalty', 'counters', 'quotes', 'songqueue', 'channelpoints', 'govee', 'billing', 'settings', 'access']
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
const ROADMAP_ENUMS = new Set<EnumName>(['tick', 'opt', 'rule', 'runtime', 'game', 'flow', 'feature', 'login', 'platform']);
type ShareName = 'ofTotal' | 'ofAccounts' | 'ofCommands' | 'ofChannels' | 'ofAttempts';
type Entry = [string, number];
type Rank = [number, number];
type Ctx = { r: AnalyticsReads; days: number; cur: DateRange; prev: DateRange; prevComplete: boolean; tracked: Day };
type DeltaSpec = { name: 'day' | 'week' | 'month' | 'since' | 'window'; days?: number };
type KpiSpec = { key: string; value: string; unit?: Phrase; change: Change; delta: DeltaSpec };
type BarSpec = {
  ids?: readonly string[];
  enum?: EnumName;
  order?: 'value' | 'numeric' | 'keep';
  limit?: number;
  name?: (id: Dim) => Name;
  secondary?: (value: Count, index: Slot, id: Dim) => Phrase | null;
};
type ModuleStats = { on: Map<string, number>; active: Map<string, number>; outputs: Map<string, number>; up: Map<string, number>; down: Map<string, number> };

const byValue = ([a, x]: Entry, [b, y]: Entry): number => y - x || a.localeCompare(b);
const SORTS = {
  value: byValue,
  numeric: ([a]: Entry, [b]: Entry): number => Number(a) - Number(b),
  keep: (): number => 0
};

// Stored lifecycle history starts months before every other metric, so it cannot mark when recording began.
const isRecorded = (row: AnalyticsRow): boolean => row.metric !== 'lifecycle';

function context(r: AnalyticsReads): Ctx {
  const { cur, prev } = windows(r.end, r.days);
  const first = bounds(r.counters)?.first;
  const tracked = bounds(r.counters.filter(isRecorded))?.first ?? cur.from;
  return { r, days: r.days, cur, prev, prevComplete: first !== undefined && first <= prev.from, tracked };
}

function since(c: Ctx, rows: AnalyticsRow[]): Day {
  const first = bounds(rows)?.first;
  return first !== undefined && first < c.tracked ? first : c.tracked;
}

function daily(c: Ctx, rows: AnalyticsRow[]): Series {
  return dailySeries(rows, c.cur, since(c, rows));
}

const counter = (c: Ctx, metric: Metric): AnalyticsRow[] => inRange(pick(c.r.counters, metric), c.cur);
const level = (c: Ctx, metric: Metric): AnalyticsRow[] => latest(pick(c.r.levels, metric));
const weekly = (c: Ctx, metric: Metric): AnalyticsRow[] => rowsAt(pick(c.r.week, metric), lastClosedStart(c.r.end, 'w'));
const get = (values: Map<string, number>, id: Dim): number => values.get(id) ?? 0;

function facts(values: Record<string, string>): Fact[] {
  return Object.entries(values).map(([key, value]) => ({ key, value }));
}

function row(id: Dim, name: Name, texts: string[]): TableRow {
  return { id, name, cells: texts.map((text) => ({ text })) };
}

function enumName(id: Dim, group?: EnumName): Name {
  const known = group !== undefined && (ENUMS[group] as readonly string[]).includes(id);
  const catalog = group !== undefined && ROADMAP_ENUMS.has(group) ? 'roadmap' : 'dim';
  return known ? { key: `admin.analytics.${catalog}.${group}.${id}` } : { text: id };
}

function ofShare(name: ShareName, whole: Count): (value: Count) => Phrase {
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
      secondary: spec.secondary?.(value, index, id) ?? null
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

function distinctKpi(c: Ctx, source: AnalyticsRow[], spec: { metric: Metric; key: Dim; unit: Unit }): Kpi {
  const rows = pick(source, spec.metric);
  const last = lastClosedStart(c.r.end, spec.unit);
  const now = valueAt(rows, last);
  const before = valueAt(rows, previousStart(last, spec.unit));
  return kpi({ key: spec.key, value: fmt(now), change: relativeChange(now, before), delta: { name: UNIT_DELTA[spec.unit] } });
}

function stickiness(c: Ctx, month: Day): number | null {
  const daily = pick(c.r.counters, 'channels.active');
  const full = monthRange(month);
  const covered: DateRange = { from: c.tracked > full.from ? c.tracked : full.from, to: full.to };
  const length = daysIn(covered).length;
  const active = valueAt(pick(c.r.month, 'channels.active'), month);
  return ratio(sum(inRange(daily, covered)), length * active);
}

function stickinessKpi(c: Ctx): Kpi {
  const last = lastClosedStart(c.r.end, 'm');
  const now = stickiness(c, last);
  const before = stickiness(c, previousStart(last, 'm'));
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
  const delta: DeltaSpec = { name: accounts.days === 1 ? 'day' : 'since', days: accounts.days };
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
    delta: { name: c.days === 1 ? 'day' : 'window', days: c.days }
  });
}

function kpis(c: Ctx): Kpi[] {
  return [
    distinctKpi(c, c.r.counters, { metric: 'channels.active', key: 'dau', unit: 'd' }),
    distinctKpi(c, c.r.week, { metric: 'channels.active', key: 'wau', unit: 'w' }),
    distinctKpi(c, c.r.month, { metric: 'channels.active', key: 'mau', unit: 'm' }),
    stickinessKpi(c),
    ...levelKpis(c),
    signupsKpi(c)
  ];
}

function stepOf(values: Count[]): (value: Count, index: Slot) => Phrase | null {
  return (value, index) =>
    index === 0
      ? null
      : {
          key: 'admin.analytics.funnel.step',
          params: { ofSignups: pct(value, values[0]), ofPrevious: pct(value, values[index - 1]) }
        };
}

const isLoss = (row: AnalyticsRow): boolean => row.dims[0] === 'account_deleted';

export function gainLoss(counters: AnalyticsRow[], range: DateRange): GainLoss {
  const inside = inRange(pick(counters, 'lifecycle'), range);
  const gainedByDay = byDay(withDim(inside, 'signup'));
  const lostByDay = byDay(inside.filter(isLoss));
  const points =
    gainedByDay.size + lostByDay.size === 0
      ? []
      : daysIn(range).map((day) => ({ at: Date.parse(day), gained: get(gainedByDay, day), lost: get(lostByDay, day) }));
  const gained = total(gainedByDay);
  const lost = total(lostByDay);
  return { points, gained, lost, net: gained - lost, lossDays: points.filter((p) => p.lost > p.gained).length };
}

function growth(c: Ctx): AnalyticsView['growth'] {
  const life = byDim(counter(c, 'lifecycle'));
  const n = (id: Dim) => get(life, id);
  const presets = byDim(counter(c, 'onboarding.preset'));
  const bot = snapshots(inRange(pick(c.r.levels, 'accounts.bot_enabled'), c.cur));
  return {
    signups: daily(c, withDim(pick(c.r.counters, 'lifecycle'), 'signup')),
    gainLoss: gainLoss(c.r.counters, c.cur),
    funnel: bars(life, { ids: FUNNEL, enum: 'lifecycle', order: 'keep', secondary: stepOf(FUNNEL.map(n)) }),
    churn: bars(life, { ids: CHURN, enum: 'lifecycle', order: 'keep' }),
    churnFacts: facts({
      netAccounts: signed(n('signup') - n('account_deleted')),
      netBot: signed(sum(bot.last) - sum(bot.first))
    }),
    presets: bars(presets, { enum: 'preset', secondary: ofShare('ofTotal', total(presets)) }),
    activationAge: bars(byDim(weekly(c, 'activation.age')), { ids: ENUMS.age, enum: 'age', order: 'keep' }),
    onboarding: bars(byDim(counter(c, 'onboarding.steps')), { ids: ENUMS.onboard, enum: 'onboard', order: 'keep' }),
    imports: dimensionTable(counter(c, 'import.runs'), ['preview_ok', 'preview_failed', 'committed', 'commit_failed']),
    onboarded: accountFacts(c, 'accounts.onboarded', 'onboarded'),
    login: bars(byDim(counter(c, 'auth.login')), { ids: ENUMS.login, enum: 'login', order: 'keep' }),
    trials: facts({ trialPromoted: fmt(n('trial_promoted')) })
  };
}

function engagement(c: Ctx): AnalyticsView['engagement'] {
  const series = (metric: Metric) => daily(c, pick(c.r.counters, metric));
  const count = (metric: Metric) => sum(counter(c, metric));
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
  const cell = (active: Count | null) =>
    active === null
      ? null
      : { percent: pct(active, line.size), count: fmt(active), heat: Math.min(1, Math.max(0, active / line.size)) };
  return { week: line.week, size: fmt(line.size), cells: line.cells.map(cell) };
}

function retention(c: Ctx): AnalyticsView['retention'] {
  const from = bounds(c.r.cohortDays)?.first ?? c.r.end;
  const lines = cohorts(c.r.cohortWeeks, c.r.cohortDays, { from, to: c.r.end }, HORIZON).slice(0, RETENTION_WEEKS);
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
    conversion: conversion(c),
    checkout: dimensionTable(counter(c, 'billing.checkout'), ['redirected', 'blocked', 'unavailable', 'rejected'], 'plan'),
    checkoutFacts: checkoutFacts(c)
  };
}

function moduleLine(name: Dim, stats: ModuleStats, accounts: Count): TableRow {
  const [on, active, up, down] = [get(stats.on, name), get(stats.active, name), get(stats.up, name), get(stats.down, name)];
  const texts = [fmt(on), pct(on, accounts), fmt(active), pct(active, on), fmt(get(stats.outputs, name)), fmt(up), fmt(down), signed(up - down)];
  return row(name, { text: name }, texts);
}

function moduleTable(c: Ctx): DataTable {
  const switches = level(c, 'modules.rows');
  const toggles = counter(c, 'module.toggles');
  const accounts = sum(level(c, 'accounts.total'));
  const on = byDim(withDim(switches, 'on', 1));
  const off = byDim(withDim(switches, 'off', 1));
  for (const name of byDim(level(c, 'modules.defaults')).keys()) {
    on.set(name, Math.max(get(on, name), accounts - get(off, name)));
  }
  const stats: ModuleStats = {
    on,
    active: byDim(weekly(c, 'module.active')),
    outputs: byDim(counter(c, 'module.events')),
    up: byDim(withDim(toggles, 'on', 1)),
    down: byDim(withDim(toggles, 'off', 1))
  };
  const names = new Set([...byDim(switches).keys(), ...on.keys(), ...stats.active.keys(), ...stats.outputs.keys(), ...byDim(toggles).keys()]);
  const ordered = [...names].sort((a, b) => get(stats.on, b) - get(stats.on, a) || a.localeCompare(b));
  return { columns: MODULE_COLUMNS, rows: ordered.map((name) => moduleLine(name, stats, accounts)) };
}

function valueAtRank(counts: Rank[], rank: Slot): number {
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
  const accounts = sum(level(c, 'accounts.total'));
  return {
    modules: moduleTable(c),
    breadth: breadthTable(breadth),
    breadthFacts: breadthFacts(breadth),
    integrations: bars(byDim(level(c, 'integrations.connected')), {
      ids: ENUMS.link,
      enum: 'link',
      secondary: ofShare('ofAccounts', accounts)
    }),
    pagesHidden: bars(byDim(level(c, 'accounts.pages_hidden')), {
      ids: ENUMS.page,
      enum: 'page',
      secondary: ofShare('ofAccounts', accounts)
    }),
    dashboard: dashboardViews(c)
  };
}

function builtIn(c: Ctx): DataTable {
  const uses = pick(c.r.counters, 'command.uses');
  const window = byDim(inRange(uses, c.cur));
  window.delete('custom');
  const start = lastClosedStart(c.r.end, 'w');
  const week = byDim(inRange(uses, { from: start, to: addDays(start, 6) }));
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

function localeName(code: Dim): Name {
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
  return {
    sizes: audienceSizes(c),
    locales: bars(locales, { name: localeName, secondary: ofShare('ofAccounts', accounts) }),
    delegating: accountFacts(c, 'accounts.delegating', 'delegating'),
    runtime: bars(byDim(level(c, 'accounts.runtime')), { ids: ENUMS.runtime, enum: 'runtime', order: 'keep' }),
    desktop: bars(byDim(level(c, 'desktop.grants')), { ids: ENUMS.platform, enum: 'platform', order: 'keep' })
  };
}

function audienceSizes(c: Ctx): Bar[] {
  const counts = byDim(weekly(c, 'channels.size'));
  const visible = new Map(ENUMS.size.map((id): Entry => [id, get(counts, id) >= 5 ? get(counts, id) : 0]));
  const result = bars(visible, { ids: ENUMS.size, enum: 'size', order: 'keep', secondary: ofShare('ofTotal', total(visible)) });
  return result.map((bar) => get(counts, bar.id) < 5 ? { ...bar, valueText: '<5', secondary: null } : bar);
}

function accountFacts(c: Ctx, metric: Metric, key: Dim): Fact[] {
  const count = sum(level(c, metric));
  const accounts = sum(level(c, 'accounts.total'));
  return facts({ [key]: fmt(count), [`${key}Share`]: pct(count, accounts) });
}

function dimensionTable(rows: AnalyticsRow[], columns: string[], group?: EnumName): DataTable {
  const ids = group ? ENUMS[group] : [...byDim(rows).keys()].sort();
  return {
    columns,
    rows: ids.map((id) => {
      const values = byDim(withDim(rows, id), 1);
      return row(id, enumName(id, group), columns.map((column) => fmt(get(values, column))));
    })
  };
}

function checkoutFacts(c: Ctx): Fact[] {
  const redirected = sum(withDim(counter(c, 'billing.checkout'), 'redirected', 1));
  const activated = sum(withDim(counter(c, 'billing.events'), 'activate'));
  return facts({ redirected: fmt(redirected), activated: fmt(activated), checkoutActivation: pct(activated, redirected) });
}

function dashboardViews(c: Ctx): Bar[] {
  const rows = counter(c, 'dashboard.views');
  const owners = byDim(withDim(rows, 'owner', 1));
  const delegates = byDim(withDim(rows, 'delegate', 1));
  const totals = byDim(rows);
  return bars(totals, {
    ids: ENUMS.section,
    enum: 'section',
    secondary: (_value, _index, id) => ({
      key: 'admin.analytics.share.roles', params: { owner: fmt(get(owners, id)), delegate: fmt(get(delegates, id)) }
    })
  });
}

function recipeOutcomes(rows: AnalyticsRow[]): DataTable {
  return {
    columns: ['runs', 'ok', 'failedShare'],
    rows: ENUMS.trigger.map((trigger) => {
      const runs = withDim(rows, trigger);
      return row(trigger, enumName(trigger, 'trigger'), [
        fmt(sum(runs)), fmt(sum(withDim(runs, 'ok', 1))), pct(sum(withDim(runs, 'failed', 1)), sum(runs))
      ]);
    })
  };
}

function recipes(c: Ctx): AnalyticsView['recipes'] {
  const runs = counter(c, 'recipe.runs');
  return {
    runs: bars(byDim(runs), { ids: ENUMS.trigger, enum: 'trigger' }),
    outcomes: recipeOutcomes(runs),
    channels: distinctKpi(c, c.r.week, { metric: 'recipe.channels', key: 'recipeChannelsWeek', unit: 'w' }),
    totals: dimensionTable(level(c, 'recipes.total'), ['on', 'off'], 'trigger')
  };
}

function providers(c: Ctx): DataTable {
  const rows = counter(c, 'provider.calls');
  return {
    columns: ['calls', 'errorShare'],
    rows: [...byDim(rows)].sort(byValue).map(([provider, count]) => {
      const ok = sum(withDim(withDim(rows, provider), 'ok', 1));
      return row(provider, { text: provider }, [fmt(count), pct(count - ok, count)]);
    })
  };
}

function reliability(c: Ctx): AnalyticsView['reliability'] {
  const songs = counter(c, 'songs.requests');
  const blocked = byDim(counter(c, 'command.blocked'));
  const attempts = sum(counter(c, 'command.uses')) + total(blocked);
  return {
    drops: dimensionTable(counter(c, 'outgress.dropped'), [...ENUMS.drop], 'send'),
    dailyDrops: daily(c, pick(c.r.counters, 'outgress.dropped')),
    providers: providers(c),
    songs: bars(byDim(songs), { ids: ENUMS.song, enum: 'song', secondary: ofShare('ofTotal', sum(songs)) }),
    songFacts: facts({ songSuccess: pct(sum(withDim(songs, 'queued')), sum(songs)) }),
    blocked: bars(blocked, { ids: ENUMS.block, enum: 'block', secondary: ofShare('ofAttempts', attempts) }),
    flags: dimensionTable(counter(c, 'automod.flags'), ['shadow', 'enforced'], 'rule')
  };
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
    audience: audience(c),
    recipes: recipes(c),
    reliability: reliability(c),
    community: community(c)
  };
}

function community(c: Ctx): AnalyticsView['community'] {
  return {
    ticks: bars(byDim(counter(c, 'timers.ticks')), { ids: ENUMS.tick, enum: 'tick', order: 'keep' }),
    timers: bars(byDim(level(c, 'timers.total')), { ids: ENUMS.opt, enum: 'opt', order: 'keep' }),
    rounds: dimensionTable(counter(c, 'games.rounds'), ['drawn', 'empty', 'cancelled', 'declined', 'no_show', 'won', 'lost'], 'game'),
    entrants: dimensionTable(counter(c, 'games.entrants'), ['1', '2_4', '5_19', '20_99', '100p'], 'game'),
    points: bars(byDim(counter(c, 'points.flows')), { ids: ENUMS.flow, enum: 'flow' }),
    rewards: dimensionTable(counter(c, 'rewards.redeems'), ['ok', 'offline', 'bad_input', 'setup', 'busy', 'unreachable'], 'feature')
  };
}
