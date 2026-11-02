// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

const TITLE_DELAY = 0.3;
const TITLE_WORD_STAGGER = 0.11;
const REVEAL_WATCHDOG_MS = 1800;

const wordsOf = (h1: HTMLElement) => Array.from(h1.querySelectorAll<HTMLElement>('[data-hero-word]'));

function revealWords(h1: HTMLElement): void {
  for (const w of wordsOf(h1)) {
    w.style.opacity = '1';
    w.style.transform = 'none';
    w.style.filter = 'none';
  }
}

function runWithWatchdog(h1: HTMLElement): void {
  let revealed = false;
  const watchdog = window.setTimeout(() => {
    revealed = true;
    revealWords(h1);
  }, REVEAL_WATCHDOG_MS);

  void import('motion')
    .then(({ animate, stagger, cubicBezier }) => {
      window.clearTimeout(watchdog);
      if (revealed) return;
      animate(
        wordsOf(h1),
        {
          transform: ['translateY(112%) rotate(3deg)', 'translateY(0%) rotate(0deg)'],
          opacity: [0, 1],
          filter: ['blur(8px)', 'blur(0px)']
        },
        {
          delay: stagger(TITLE_WORD_STAGGER, { startDelay: TITLE_DELAY }),
          duration: 0.9,
          ease: cubicBezier(0.22, 1, 0.36, 1)
        }
      );
    })
    .catch(() => {
      window.clearTimeout(watchdog);
      revealWords(h1);
    });
}

export function playHeroTitle(): void {
  const h1 = document.querySelector<HTMLElement>('[data-hero-title]');
  if (!h1 || h1.dataset.heroMotionReady === 'true') return;
  h1.dataset.heroMotionReady = 'true';
  if (prefersReducedMotion()) return;
  if (document.hidden) return revealWords(h1);
  runWithWatchdog(h1);
}
