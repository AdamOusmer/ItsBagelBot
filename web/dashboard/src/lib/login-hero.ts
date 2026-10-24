// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

type Motion = typeof import('motion');

const LINE_DELAY_BASE = 0.35;
const LINE_DELAY_STEP = 0.16;
const GLYPH_STAGGER = 0.024;
const REVEAL_WATCHDOG_MS = 1800;

const glyphsOf = (root: ParentNode) => Array.from(root.querySelectorAll<HTMLElement>('[data-hero-glyph]'));

function revealGlyphs(h1: HTMLElement): void {
  for (const g of glyphsOf(h1)) {
    g.style.opacity = '1';
    g.style.transform = 'none';
    g.style.filter = 'none';
  }
}

function animateLine({ animate, stagger, cubicBezier }: Motion, line: HTMLElement, lineIndex: number): void {
  const all = glyphsOf(line);
  if (!all.length) return;
  const easeOutQuint = cubicBezier(0.22, 1, 0.36, 1);
  const easeBackOut = cubicBezier(0.34, 1.56, 0.64, 1);
  const lineDelay = LINE_DELAY_BASE + lineIndex * LINE_DELAY_STEP;
  const period = all[all.length - 1];
  const letters = all.slice(0, -1);

  animate(
    letters,
    {
      transform: ['translateY(118%) rotate(6deg)', 'translateY(0%) rotate(0deg)'],
      opacity: [0, 1],
      filter: ['blur(6px)', 'blur(0px)']
    },
    {
      delay: stagger(GLYPH_STAGGER, { startDelay: lineDelay }),
      duration: 0.8,
      ease: easeOutQuint
    }
  );

  const periodDelay = lineDelay + letters.length * GLYPH_STAGGER + 0.18;
  animate(
    period,
    {
      transform: ['translateY(0%) scale(0)', 'translateY(0%) scale(1)'],
      opacity: [0, 1]
    },
    { delay: periodDelay, duration: 0.55, ease: easeBackOut }
  );
  animate(
    period,
    {
      textShadow: [
        '0 0 0px rgba(245, 230, 207, 0)',
        '0 0 28px rgba(245, 230, 207, 0.85)',
        '0 0 0px rgba(245, 230, 207, 0)'
      ]
    },
    { delay: periodDelay, duration: 0.9, ease: 'easeOut' }
  );
}

function playLines(motion: Motion, h1: HTMLElement): void {
  h1.querySelectorAll<HTMLElement>('[data-hero-line]').forEach((line, lineIndex) => animateLine(motion, line, lineIndex));
}

function armedTitle(): HTMLElement | null {
  const h1 = document.querySelector<HTMLElement>('[data-hero-title]');
  if (!h1 || h1.dataset.heroMotionReady === 'true') return null;
  h1.dataset.heroMotionReady = 'true';
  return h1;
}

function runWithWatchdog(h1: HTMLElement): void {
  let revealed = false;
  const watchdog = window.setTimeout(() => {
    revealed = true;
    revealGlyphs(h1);
  }, REVEAL_WATCHDOG_MS);

  void import('motion')
    .then((motion) => {
      window.clearTimeout(watchdog);
      if (!revealed) playLines(motion, h1);
    })
    .catch(() => {
      window.clearTimeout(watchdog);
      revealGlyphs(h1);
    });
}

export function playHeroTitle(): void {
  const h1 = armedTitle();
  if (!h1 || prefersReducedMotion()) return;
  if (document.hidden) return revealGlyphs(h1);
  runWithWatchdog(h1);
}
