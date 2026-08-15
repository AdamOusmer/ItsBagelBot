// Public (unauthenticated) global counters for /stats.
//
// Two lifetime, bot-scope counters live in the loyalty service — the same
// counter store the dashboard's per-channel counters use, but written under the
// reserved '0' user id so they aggregate the whole fleet rather than a channel:
//
//   messages_processed — every chat message the ingress path has handled
//   events_processed   — every Twitch event (subs, cheers, follows, …)
//
// The page is public, so this read must be cheap and must never be able to
// error a render. Three rules follow from that:
//
//   1. One cached snapshot key for BOTH counters (POLICY.live: 1s fresh, 2s
//      SWR, single-flight). A traffic spike on an anonymous page therefore
//      costs at most ~1 RPC pair per second per pod, not one per visitor.
//   2. A counter loyalty does not have yet reads as 0, not as an error. The
//      writer may ship after this page does; an absent counter is honestly 0.
//   3. Loyalty unreachable (timeout / no responders) degrades to zeros with
//      `degraded: true` so the page can say so instead of inventing numbers.
//
// Rates are derived here, not stored: each fresh snapshot diffs against the
// previous one and divides by the wall time between them (same shape as the
// admin lane sampler). Sampling is per-pod and per-process, which is fine — the
// counters are fleet-global, so any pod's delta measures the same fleet.
import { rpc } from '@bagel/shared/server/nats';
import { POLICY } from '@bagel/shared/server/cache-keys';
import { fabric, SUB } from './services';

const CACHE_KEY = 'public-stats:global';

// Bot-scope counters are keyed to the reserved '0' user, not a broadcaster.
const BOT_SCOPE_USER = '0';
const COUNTER_MESSAGES = 'messages_processed';
const COUNTER_EVENTS = 'events_processed';

const RPC_TIMEOUT_MS = 4000;

// Below this the divisor is too small to trust: a sub-second gap turns a batched
// counter flush into a fake spike. Keep the previous rates instead.
const MIN_SAMPLE_MS = 1000;

export interface PublicStats {
  messages_total: number;
  events_total: number;
  /** null until a second sample exists (or while loyalty is unreachable). */
  msg_rate: number | null;
  event_rate: number | null;
  degraded: boolean;
}

interface CounterWire {
  name?: string;
  scope?: string;
  value?: number;
}

interface LoyaltyReplyWire {
  counter?: CounterWire;
  found?: boolean;
  error?: string;
}

/**
 * Read one bot-scope counter.
 *
 * Returns the value, or null when loyalty could not answer. A counter that was
 * never created is NOT an error: loyalty replies `found: false` with no counter,
 * which reads as an honest 0 (the writer may ship after this page does). An
 * RpcError, by contrast, is loyalty reporting a real failure — that degrades
 * the snapshot rather than rendering zeroed lifetime totals on a healthy page.
 */
async function counterValue(name: string): Promise<number | null> {
  try {
    const reply = await rpc<LoyaltyReplyWire>(
      `${SUB.loyalty}.counter.get`,
      { user_id: BOT_SCOPE_USER, name },
      RPC_TIMEOUT_MS
    );
    const raw = reply.counter?.value;
    return Number.isFinite(raw) ? Number(raw) : 0;
  } catch {
    return null;
  }
}

interface Sample {
  messages: number;
  events: number;
  at: number;
}

// Per-process rate baseline. Only ever advanced from a non-degraded snapshot, so
// an outage cannot poison the next real delta with zeros.
let prev: Sample | null = null;
let lastRates: { msg: number | null; event: number | null } = { msg: null, event: null };

function perSecond(current: number, previous: number, secs: number): number {
  // Counters are monotonic; clamp so a counter reset (service redeploy, manual
  // set) reads as a pause rather than a negative rate.
  return Math.max(current - previous, 0) / secs;
}

/**
 * Fold a fresh reading into the rate baseline and return the derived rates.
 * The first reading of a process establishes the baseline and yields nulls —
 * there is no honest rate to show from a single sample.
 */
function sampleRates(messages: number, events: number, now: number): { msg: number | null; event: number | null } {
  const before = prev;
  if (!before) {
    prev = { messages, events, at: now };
    return lastRates;
  }
  const elapsedMs = now - before.at;
  if (elapsedMs < MIN_SAMPLE_MS) return lastRates;

  const secs = elapsedMs / 1000;
  prev = { messages, events, at: now };
  lastRates = {
    msg: perSecond(messages, before.messages, secs),
    event: perSecond(events, before.events, secs)
  };
  return lastRates;
}

function degradedStats(): PublicStats {
  return { messages_total: 0, events_total: 0, msg_rate: null, event_rate: null, degraded: true };
}

// DEMO=1 serves a synthetic, steadily-growing snapshot so the page can be
// previewed without a fleet behind it (same convention as demo-notifications).
const DEMO_EPOCH = Date.parse('2026-01-01T00:00:00Z');

function demoStats(now: number): PublicStats {
  const secs = (now - DEMO_EPOCH) / 1000;
  const msgRate = 84 + 12 * Math.sin(now / 45_000);
  const eventRate = 137 + 18 * Math.sin(now / 60_000 + 1.7);
  return {
    messages_total: Math.floor(1_508_000_000 + secs * 84),
    events_total: Math.floor(2_430_000_000 + secs * 137),
    msg_rate: msgRate,
    event_rate: eventRate,
    degraded: false
  };
}

async function loadStats(): Promise<PublicStats> {
  const [messages, events] = await Promise.all([counterValue(COUNTER_MESSAGES), counterValue(COUNTER_EVENTS)]);
  if (messages === null || events === null) return degradedStats();

  const rates = sampleRates(messages, events, Date.now());
  return {
    messages_total: messages,
    events_total: events,
    msg_rate: rates.msg,
    event_rate: rates.event,
    degraded: false
  };
}

/**
 * The whole public snapshot: both lifetime totals plus their derived rates.
 * Never rejects — a total failure to reach loyalty resolves to zeros with
 * `degraded: true`.
 */
export async function publicStats(): Promise<PublicStats> {
  if (process.env.DEMO === '1') return demoStats(Date.now());
  try {
    return await fabric.readKey(CACHE_KEY, POLICY.live, loadStats);
  } catch {
    return degradedStats();
  }
}
