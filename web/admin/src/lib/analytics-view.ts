// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type AnalyticsRow = { start: string; metric: string; dims: string[]; value: number; suppressed?: boolean };
export type Cell = { value: number; partial: boolean };
export type Line = { label: string; cell: Cell };

const EMPTY: Cell = { value: 0, partial: false };

function add(a: Cell, row: AnalyticsRow): Cell {
  return { value: a.value + row.value, partial: a.partial || !!row.suppressed };
}

export function cellText(cell: Cell): string {
  if (cell.partial && cell.value === 0) return '<10';
  const n = cell.value.toLocaleString('en-US');
  return cell.partial ? `${n}+` : n;
}

export function total(rows: AnalyticsRow[], metric: string): Cell {
  return rows.filter((r) => r.metric === metric).reduce(add, EMPTY);
}

export function byDim(rows: AnalyticsRow[], metric: string, index = 0, limit = 50): Line[] {
  const sums = new Map<string, Cell>();
  for (const row of rows) {
    if (row.metric !== metric) continue;
    const label = row.dims[index] ?? '';
    sums.set(label, add(sums.get(label) ?? EMPTY, row));
  }
  return [...sums]
    .map(([label, cell]) => ({ label, cell }))
    .sort((a, b) => b.cell.value - a.cell.value || a.label.localeCompare(b.label))
    .slice(0, limit);
}

export function latest(rows: AnalyticsRow[], metric: string): AnalyticsRow[] {
  const own = rows.filter((r) => r.metric === metric);
  const last = own.reduce((max, r) => (r.start > max ? r.start : max), '');
  return own.filter((r) => r.start === last);
}

export function daily(rows: AnalyticsRow[], metric: string): number[] {
  const sums = new Map<string, number>();
  for (const row of rows) {
    if (row.metric === metric) sums.set(row.start, (sums.get(row.start) ?? 0) + row.value);
  }
  return [...sums].sort(([a], [b]) => a.localeCompare(b)).map(([, n]) => n);
}

function exact(part: Cell, whole: Cell): boolean {
  return whole.value !== 0 && !whole.partial && !part.partial;
}

export function share(part: Cell, whole: Cell): string {
  if (!exact(part, whole)) return '-';
  return `${Math.round((part.value / whole.value) * 100)}%`;
}

const DAY_MS = 86_400_000;

function mondayOf(date: Date): Date {
  const day = new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), date.getUTCDate()));
  return new Date(day.getTime() - ((day.getUTCDay() + 6) % 7) * DAY_MS);
}

export function isoWeek(dateOnly: string): string {
  const thursday = new Date(mondayOf(new Date(`${dateOnly}T00:00:00Z`)).getTime() + 3 * DAY_MS);
  const year = thursday.getUTCFullYear();
  const week = Math.floor((thursday.getTime() - Date.UTC(year, 0, 1)) / (7 * DAY_MS)) + 1;
  return `${year}-W${String(week).padStart(2, '0')}`;
}

export function weekStart(label: string): string {
  const [year, week] = label.split('-W').map(Number);
  const jan4 = new Date(Date.UTC(year, 0, 4));
  return new Date(mondayOf(jan4).getTime() + (week - 1) * 7 * DAY_MS).toISOString().slice(0, 10);
}

export type CohortRow = { week: string; size: Cell; cells: (Cell | null)[] };

function cohortSizes(dayRows: AnalyticsRow[]): Map<string, Cell> {
  const sizes = new Map<string, Cell>();
  for (const row of dayRows) {
    if (row.metric !== 'lifecycle' || row.dims[0] !== 'signup') continue;
    const week = isoWeek(row.start);
    sizes.set(week, add(sizes.get(week) ?? EMPTY, row));
  }
  return sizes;
}

function cohortCells(weekRows: AnalyticsRow[], horizon: number): Map<string, (Cell | null)[]> {
  const cells = new Map<string, (Cell | null)[]>();
  for (const row of weekRows) {
    if (row.metric !== 'cohort.active') continue;
    const signup = row.dims[0];
    const offset = Math.round((Date.parse(row.start) - Date.parse(weekStart(signup))) / (7 * DAY_MS));
    if (offset < 0 || offset >= horizon) continue;
    const line = cells.get(signup) ?? Array<Cell | null>(horizon).fill(null);
    line[offset] = { value: row.value, partial: !!row.suppressed };
    cells.set(signup, line);
  }
  return cells;
}

// Sizes come from signup counts, so deleted accounts still count in their cohort.
export function cohorts(weekRows: AnalyticsRow[], dayRows: AnalyticsRow[], horizon = 8): CohortRow[] {
  const sizes = cohortSizes(dayRows);
  const cells = cohortCells(weekRows, horizon);
  const empty = () => Array<Cell | null>(horizon).fill(null);
  return [...sizes.keys()]
    .sort()
    .reverse()
    .map((week) => ({ week, size: sizes.get(week) ?? EMPTY, cells: cells.get(week) ?? empty() }));
}
