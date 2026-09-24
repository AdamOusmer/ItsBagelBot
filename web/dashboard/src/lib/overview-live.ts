// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type StreamMeta = {
  live: boolean;
  known: boolean;
  title: string;
  gameName: string;
  startedAt: string | null;
  endedAt: string | null;
  viewers: number;
  peakViewers: number;
  lastDurationMin: number;
  ok: boolean;
};

export type StreamCounters = {
  messages: number;
  answered: number;
  modActions: number;
  ok: boolean;
};

export type ChatVolume = {
  buckets: number[];
  commandTicks: number[];
  now: number;
  peak: number;
  ok: boolean;
};

export type ActivityKind =
  | 'command'
  | 'timer'
  | 'automod'
  | 'reward'
  | 'loyalty'
  | 'event'
  | 'queue';

export type ActivityRow = {
  id: string;
  kind: ActivityKind;
  text: string;
  meta: string;
  at: string;
};

export type ActivityFeed = {
  rows: ActivityRow[];
  medianMs: number | null;
  dropped: number;
  ok: boolean;
};

export type AnsweredCommand = { name: string; count: number };
export type AnsweredTonight = { commands: AnsweredCommand[]; ok: boolean };

export function degradedStreamMeta(): StreamMeta {
  return {
    live: false,
    known: false,
    title: '',
    gameName: '',
    startedAt: null,
    endedAt: null,
    viewers: 0,
    peakViewers: 0,
    lastDurationMin: 0,
    ok: false
  };
}

export function degradedStreamCounters(): StreamCounters {
  return { messages: 0, answered: 0, modActions: 0, ok: false };
}

export function degradedChatVolume(): ChatVolume {
  return { buckets: [], commandTicks: [], now: 0, peak: 0, ok: false };
}

export function degradedActivityFeed(): ActivityFeed {
  return { rows: [], medianMs: null, dropped: 0, ok: false };
}

export function degradedAnsweredTonight(): AnsweredTonight {
  return { commands: [], ok: false };
}

export function allRead<T>(vals: (T | null)[]): vals is T[] {
  return vals.every((v) => v !== null);
}

export function finiteOrNull(raw: unknown): number | null {
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

export function formatDuration(minutes: number): string {
  const m = Math.max(0, Math.floor(minutes));
  const h = Math.floor(m / 60);
  return h > 0 ? `${h}h ${String(m % 60).padStart(2, '0')}m` : `${m}m`;
}

export function minutesSince(iso: string | null, now: number): number {
  if (!iso) return 0;
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return 0;
  return Math.max(0, Math.floor((now - then) / 60_000));
}

export function clockFace(iso: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}
