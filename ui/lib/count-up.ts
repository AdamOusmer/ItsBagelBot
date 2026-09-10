// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

/** What a count-up needs to know about the text it is animating. */
type Parsed = { target: number; suffix: string; grouped: boolean };

/**
 * Split "12,904 msg" into the number to count to, the text to keep after it,
 * and whether to re-group the digits on the way up. Returns null for anything
 * that is not a positive number ("Live", "VIP", "0", "—").
 *
 * Single bounded quantifier for the numeric prefix, then a plain slice for the
 * suffix: avoids a regex whose two groups (`[\d,]+` and `.*`) both match ','
 * and so overlap, which is what trips ReDoS scanners even though the trailing
 * `.*$` can't itself fail to match here.
 */
function parseCount(raw: string): Parsed | null {
  const m = raw.match(/^[\d,]+/);
  if (!m) return null;
  const digits = m[0];
  const target = Number(digits.replace(/,/g, ''));
  if (!Number.isFinite(target) || target <= 0) return null;
  return { target, suffix: raw.slice(digits.length), grouped: digits.includes(',') };
}

/** False on the server and under prefers-reduced-motion. */
function animates(): boolean {
  if (typeof window === 'undefined') return false;
  return !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

function format(n: number, grouped: boolean): string {
  return grouped ? Math.round(n).toLocaleString() : String(Math.round(n));
}

/**
 * Count-up readout: numeric text ticks from 0 to its real value on mount, like
 * a meter settling. Non-numeric strings ("Live", "VIP") and reduced-motion
 * environments render as-is.
 *
 * Moved out of web/kit/lib/actions.ts because StatTile moved here and this is
 * the only thing that drives it. @bagel/kit re-exports it from `./actions` so
 * the barrel's `countUp` keeps working unchanged.
 *
 * Split into parseCount / animates / format on the way over. It arrived as one
 * function at cyclomatic 10 against a threshold of 9, carried in on the move;
 * the three helpers are the seams it already had -- decide whether to run,
 * read the number, print the number -- rather than an arbitrary cut to get
 * under a limit.
 *
 * Written as `(node, opts) => { destroy() }` -- which is Svelte's action shape
 * -- WITHOUT importing svelte's `Action` type, so this file stays inside the
 * framework-free half of the package (ui/scripts/assert-framework-free.mjs
 * enforces that). The shape costs nothing to a non-Svelte caller: it is a
 * mount function that returns a teardown, which is the lifecycle every engine
 * in ui/lib uses anyway.
 *
 * Still on its own requestAnimationFrame rather than the shared raf-loop
 * scheduler: that scheduler does not exist on this branch yet (it lands with
 * the motion PR). The loop already self-terminates when p reaches 1, so the
 * cost of waiting is one extra rAF subscriber for the ~900ms a stat grid takes
 * to settle, not a permanently running frame loop.
 */
export function countUp(
  node: HTMLElement,
  opts?: { durationMs?: number },
): { destroy(): void } | undefined {
  if (!animates()) return;

  const parsed = parseCount((node.textContent ?? '').trim());
  if (!parsed) return;

  const duration = opts?.durationMs ?? 900;
  const t0 = performance.now();
  let raf = 0;

  const tick = (t: number) => {
    const p = Math.min(1, (t - t0) / duration);
    const eased = 1 - Math.pow(1 - p, 4); // ease-out-quart: fast rise, soft landing
    node.textContent = format(parsed.target * eased, parsed.grouped) + parsed.suffix;
    if (p < 1) raf = requestAnimationFrame(tick);
  };
  raf = requestAnimationFrame(tick);

  return {
    destroy() {
      if (raf) cancelAnimationFrame(raf);
    },
  };
}
