// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

const ACTIVE = 'is-active';

export function mountHashActive(root: ParentNode): () => void {
  const sync = () => {
    const hash = window.location.hash;
    for (const link of root.querySelectorAll<HTMLAnchorElement>('a[href^="#"]')) {
      const current = link.getAttribute('href') === hash;
      link.classList.toggle(ACTIVE, current);
      if (current) link.setAttribute('aria-current', 'location');
      else link.removeAttribute('aria-current');
    }
  };

  sync();
  window.addEventListener('hashchange', sync);
  return () => window.removeEventListener('hashchange', sync);
}
