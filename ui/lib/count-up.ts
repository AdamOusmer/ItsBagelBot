// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

type Parsed = { target: number; suffix: string; grouped: boolean };

function parseCount(raw: string): Parsed | null {
  const m = raw.match(/^[\d,]+/);
  if (!m) return null;
  const digits = m[0];
  const target = Number(digits.replace(/,/g, ''));
  if (!Number.isFinite(target) || target <= 0) return null;
  return { target, suffix: raw.slice(digits.length), grouped: digits.includes(',') };
}

function animates(): boolean {
  if (typeof window === 'undefined') return false;
  return !window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

function format(n: number, grouped: boolean): string {
  return grouped ? Math.round(n).toLocaleString() : String(Math.round(n));
}

function easeOutQuart(p: number): number {
  return 1 - Math.pow(1 - p, 4);
}

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
    const eased = easeOutQuart(p);
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
