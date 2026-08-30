// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The Overview redesign's live panels, as one frozen contract.
//
// Every panel below is fed by a backend lane that ships separately. This file
// exists so those lanes never touch the page, the loader or each other: the
// types and the degraded defaults land once, the page renders against them
// immediately, and each lane later swaps a `degraded*()` default for a real
// read without editing a shared file.
//
// The honesty rule is the same one the existing digests follow (see
// NeedsAttention's guard comments): a failed read must be distinguishable from
// a genuinely empty state. Zeros alone cannot do that, so every shape carries
// `ok`. A panel whose `ok` is false says so; it never renders 0 as if it were
// measured.

/** Live state of the channel's current or most recent stream. */
export type StreamMeta = {
  live: boolean;
  /** False when the projector has never seen a stream event for this channel. */
  known: boolean;
  title: string;
  gameName: string;
  /** ISO-8601, or null when unknown. */
  startedAt: string | null;
  endedAt: string | null;
  viewers: number;
  peakViewers: number;
  /** Duration of the last completed stream, in minutes. 0 when unknown. */
  lastDurationMin: number;
  ok: boolean;
};

/** Counters scoped to the current stream, not lifetime totals. */
export type StreamCounters = {
  messages: number;
  answered: number;
  modActions: number;
  ok: boolean;
};

/** Chat volume across the stream, one bucket per minute, oldest first. */
export type ChatVolume = {
  buckets: number[];
  /** Indices into `buckets` where the bot answered a command. */
  commandTicks: number[];
  /** Messages per minute in the newest bucket. */
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
  /** Right-aligned detail: a latency, a point total, a queue position. */
  meta: string;
  /** ISO-8601 timestamp; the component renders the clock face. */
  at: string;
};

export type ActivityFeed = {
  rows: ActivityRow[];
  /** Median command answer time in ms, or null before enough samples exist. */
  medianMs: number | null;
  /** Events the pipeline hook shed under backpressure. */
  dropped: number;
  ok: boolean;
};

/** Per-command answer counts for the current stream, highest first. */
export type AnsweredCommand = { name: string; count: number };
export type AnsweredTonight = { commands: AnsweredCommand[]; ok: boolean };

// ── degraded defaults ────────────────────────────────────────────────────────
// Each lane replaces its default with a real read. Until then the page renders
// a truthful "not measured yet" rather than a confident zero, and the redesign
// ships without waiting on any of them.

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

// ── shared formatting ────────────────────────────────────────────────────────

/** "3h 42m" / "42m" from a minute count. Callers pass 0 for unknown. */
export function formatDuration(minutes: number): string {
  const m = Math.max(0, Math.floor(minutes));
  const h = Math.floor(m / 60);
  return h > 0 ? `${h}h ${String(m % 60).padStart(2, '0')}m` : `${m}m`;
}

/** Minutes elapsed since an ISO timestamp, or 0 when it is absent/unparsable. */
export function minutesSince(iso: string | null, now: number): number {
  if (!iso) return 0;
  const then = Date.parse(iso);
  if (!Number.isFinite(then)) return 0;
  return Math.max(0, Math.floor((now - then) / 60_000));
}

/** "17:22" in the viewer's locale-independent 24h clock face. */
export function clockFace(iso: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}
