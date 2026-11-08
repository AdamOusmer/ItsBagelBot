// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { prefersReducedMotion } from '@bagel/ui/lib/motion-query';

const TITLE_DELAY = 0.3;
const TITLE_WORD_STAGGER = 0.11;
const REVEAL_WATCHDOG_MS = 1800;

const played = new WeakSet<HTMLElement>();

function revealWords(words: HTMLElement[]): void {
  for (const w of words) {
    w.style.opacity = '1';
    w.style.transform = 'none';
    w.style.filter = 'none';
  }
}

function runWithWatchdog(words: HTMLElement[]): void {
  let revealed = false;
  const watchdog = window.setTimeout(() => {
    revealed = true;
    revealWords(words);
  }, REVEAL_WATCHDOG_MS);

  void import('motion')
    .then(({ animate, stagger, cubicBezier }) => {
      window.clearTimeout(watchdog);
      if (revealed) return;
      animate(
        words,
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
      revealWords(words);
    });
}

export function playHeroTitle(h1: HTMLElement | null, words: HTMLElement[]): void {
  if (!h1 || played.has(h1)) return;
  played.add(h1);
  if (prefersReducedMotion()) return;
  if (document.hidden) return revealWords(words);
  runWithWatchdog(words);
}
