// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Marks the in-page anchor matching the current hash.
//
// The hash, NOT the scroll position: `:target` lives on the section, so the
// link that should be lit is whatever the URL already says. A scroll-position
// spy has to guess which of several visible sections is "the" one and gets it
// wrong whenever the last section is shorter than the viewport -- which is why
// the version this replaces (a rAF scrollspy) lit the wrong entry after a
// search dropped sections from the page.
//
// `hashchange` rather than a router hook: an in-page click is not a navigation
// to SvelteKit or to Astro's router, so neither fires. It IS a hashchange in
// every browser.

const ACTIVE = 'is-active';

/**
 * Watch the hash and mark matching anchors inside `root`. Returns `dispose()`.
 *
 * Writes `aria-current="location"` alongside the class, which is the value for
 * "a section of the current page" -- `page` would claim this link points at
 * the document you are already on, and a screen reader reads that as a
 * navigation error rather than as a position.
 */
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
