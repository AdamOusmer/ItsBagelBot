// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Day = string;
export type Metric = string;
export type Dim = string;
export type Count = number;
export type Slot = number;
export type Percent = number;
export type AnalyticsRow = { start: Day; metric: Metric; dims: Dim[]; value: Count };
export type DateRange = { from: Day; to: Day };
export type Unit = 'd' | 'w' | 'm';
export type Direction = 'up' | 'down' | 'flat';
export type Change = { text: string; trend: Direction } | null;
export type Daily = { values: number[]; labels: string[]; ticks: number[]; average: string; peak: string };
export type Snapshots = { first: AnalyticsRow[]; last: AnalyticsRow[]; days: number };
export type CohortRow = { week: string; size: number; cells: (number | null)[] };

const DAY_MS = 86_400_000;
const WEEK_MS = 7 * DAY_MS;
const UNIT_DAYS = { d: 1, w: 7 } as const;

export function addDays(day: Day, n: Count): Day {
  return new Date(Date.parse(day) + n * DAY_MS).toISOString().slice(0, 10);
}

export function daysBetween(from: Day, to: Day): Count {
  return Math.round((Date.parse(to) - Date.parse(from)) / DAY_MS);
}

export function lastClosedDay(now: Date): Day {
  return addDays(now.toISOString().slice(0, 10), -1);
}

export function daysIn(range: DateRange): Day[] {
  const count = daysBetween(range.from, range.to) + 1;
  return Array.from({ length: Math.max(count, 0) }, (_, i) => addDays(range.from, i));
}

export function windows(end: Day, days: Count): { cur: DateRange; prev: DateRange } {
  return {
    cur: { from: addDays(end, 1 - days), to: end },
    prev: { from: addDays(end, 1 - 2 * days), to: addDays(end, -days) }
  };
}

export function monthRange(start: Day): DateRange {
  const d = new Date(Date.parse(start));
  const next = Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1);
  return { from: start, to: new Date(next - DAY_MS).toISOString().slice(0, 10) };
}

export function previousStart(start: Day, unit: Unit): Day {
  if (unit !== 'm') return addDays(start, -UNIT_DAYS[unit]);
  const d = new Date(Date.parse(start));
  d.setUTCMonth(d.getUTCMonth() - 1);
  return d.toISOString().slice(0, 10);
}

export function pick(rows: AnalyticsRow[], metric: Metric): AnalyticsRow[] {
  return rows.filter((r) => r.metric === metric);
}

export function withDim(rows: AnalyticsRow[], dim: Dim, at: Slot = 0): AnalyticsRow[] {
  return rows.filter((r) => r.dims[at] === dim);
}

export function inRange(rows: AnalyticsRow[], range: DateRange): AnalyticsRow[] {
  return rows.filter((r) => r.start >= range.from && r.start <= range.to);
}

export function sum(rows: AnalyticsRow[]): Count {
  return rows.reduce((acc, r) => acc + r.value, 0);
}

export function total(values: Map<Dim, Count>): Count {
  return [...values.values()].reduce((acc, n) => acc + n, 0);
}

export function byDim(rows: AnalyticsRow[], at: Slot = 0): Map<Dim, Count> {
  const sums = new Map<string, number>();
  for (const row of rows) {
    const dim = row.dims[at] ?? '';
    sums.set(dim, (sums.get(dim) ?? 0) + row.value);
  }
  return sums;
}

export function bounds(rows: AnalyticsRow[]): { first: Day; last: Day } | null {
  if (rows.length === 0) return null;
  let first = rows[0].start;
  let last = first;
  for (const { start } of rows) {
    if (start < first) first = start;
    if (start > last) last = start;
  }
  return { first, last };
}

export function rowsAt(rows: AnalyticsRow[], start: Day): AnalyticsRow[] {
  return rows.filter((r) => r.start === start);
}

export function valueAt(rows: AnalyticsRow[], start: Day): Count {
  return sum(rowsAt(rows, start));
}

export function latest(rows: AnalyticsRow[]): AnalyticsRow[] {
  const edge = bounds(rows);
  return edge ? rowsAt(rows, edge.last) : [];
}

export function snapshots(rows: AnalyticsRow[]): Snapshots {
  const edge = bounds(rows);
  if (!edge) return { first: [], last: [], days: 0 };
  return { first: rowsAt(rows, edge.first), last: rowsAt(rows, edge.last), days: daysBetween(edge.first, edge.last) };
}

const MONTHLY_TICKS_FROM = 365;

function isTick(day: Day, monthly: boolean): boolean {
  return monthly ? day.endsWith('-01') : new Date(Date.parse(day)).getUTCDay() === 1;
}

function averageText(n: number): string {
  return n < 10 ? n.toFixed(1) : fmt(Math.round(n));
}

export function byDay(rows: AnalyticsRow[]): Map<Day, Count> {
  const sums = new Map<Day, Count>();
  for (const row of rows) sums.set(row.start, (sums.get(row.start) ?? 0) + row.value);
  return sums;
}

export function dailySeries(rows: AnalyticsRow[], range: DateRange): Daily {
  const inside = inRange(rows, range);
  if (inside.length === 0) return { values: [], labels: [], ticks: [], average: '-', peak: '-' };
  const sums = byDay(inside);
  const days = daysIn(range);
  const values = days.map((d) => sums.get(d) ?? 0);
  const monthly = days.length >= MONTHLY_TICKS_FROM;
  return {
    values,
    labels: days.map(dateLabel),
    ticks: days.flatMap((d, i) => (isTick(d, monthly) ? [i] : [])),
    average: averageText(sum(inside) / values.length),
    peak: fmt(Math.max(...values))
  };
}

export function fmt(n: Count): string {
  return n.toLocaleString('en-US');
}

export function decimal(n: Count): string {
  return n.toFixed(1);
}

export function signed(n: Count): string {
  return `${n > 0 ? '+' : ''}${fmt(n)}`;
}

export function dateLabel(day: Day): string {
  return new Date(Date.parse(day)).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    timeZone: 'UTC'
  });
}

export function ratio(part: Count, whole: Count): Percent | null {
  return whole === 0 ? null : (part / whole) * 100;
}

export function pctText(value: Percent): string {
  if (value === 0) return '0%';
  const tenth = Math.round(value * 10) / 10;
  if (tenth < 10) return `${tenth.toFixed(1)}%`;
  const rounded = Math.round(value);
  if (value < 100 && rounded >= 100) return `${value.toFixed(1) === '100.0' ? '99.9' : value.toFixed(1)}%`;
  return `${rounded}%`;
}

export function pct(part: Count, whole: Count): string {
  const value = ratio(part, whole);
  return value === null ? '-' : pctText(value);
}

function trendOf(n: Count): Direction {
  if (n > 0) return 'up';
  if (n < 0) return 'down';
  return 'flat';
}

export function relativeChange(current: Count, previous: Count): Change {
  if (previous === 0) return null;
  const n = Math.round(((current - previous) / previous) * 100);
  return { text: `${n > 0 ? '+' : ''}${n}%`, trend: trendOf(n) };
}

export function pointsChange(current: Percent | null, previous: Percent | null): Change {
  if (current === null || previous === null) return null;
  const n = Math.round((current - previous) * 10) / 10;
  return { text: `${n > 0 ? '+' : ''}${n.toFixed(1)} pts`, trend: trendOf(n) };
}

function mondayOf(date: Date): Date {
  const day = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  return new Date(day.getTime() - ((day.getUTCDay() + 6) % 7) * DAY_MS);
}

export function isoWeek(dateOnly: Day): string {
  const thursday = new Date(mondayOf(new Date(`${dateOnly}T00:00:00Z`)).getTime() + 3 * DAY_MS);
  const year = thursday.getUTCFullYear();
  const week = Math.floor((thursday.getTime() - Date.UTC(year, 0, 1)) / WEEK_MS) + 1;
  return `${year}-W${String(week).padStart(2, '0')}`;
}

export function weekStart(label: string): Day {
  const [year, week] = label.split('-W').map(Number);
  const jan4 = new Date(Date.UTC(year, 0, 4));
  return new Date(mondayOf(jan4).getTime() + (week - 1) * WEEK_MS).toISOString().slice(0, 10);
}

function cohortSizes(dayRows: AnalyticsRow[]): Map<string, number> {
  const sizes = new Map<string, number>();
  for (const row of withDim(pick(dayRows, 'lifecycle'), 'signup')) {
    const week = isoWeek(row.start);
    sizes.set(week, (sizes.get(week) ?? 0) + row.value);
  }
  return sizes;
}

function cohortCells(weekRows: AnalyticsRow[], horizon: Slot): Map<string, (Count | null)[]> {
  const cells = new Map<string, (number | null)[]>();
  for (const row of pick(weekRows, 'cohort.active')) {
    const signup = row.dims[0];
    const offset = Math.round((Date.parse(row.start) - Date.parse(weekStart(signup))) / WEEK_MS);
    if (offset < 0 || offset >= horizon) continue;
    const line = cells.get(signup) ?? Array<number | null>(horizon).fill(null);
    line[offset] = row.value;
    cells.set(signup, line);
  }
  return cells;
}

export function cohorts(weekRows: AnalyticsRow[], dayRows: AnalyticsRow[], horizon: Slot = 8): CohortRow[] {
  const sizes = cohortSizes(dayRows);
  const cells = cohortCells(weekRows, horizon);
  return [...sizes]
    .filter(([, size]) => size > 0)
    .sort(([a], [b]) => b.localeCompare(a))
    .map(([week, size]) => ({ week, size, cells: cells.get(week) ?? Array<number | null>(horizon).fill(null) }));
}
