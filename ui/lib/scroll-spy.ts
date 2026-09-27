// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

function atDocumentBottom(): boolean {
  const scroller = document.scrollingElement;
  if (!scroller || scroller.scrollTop <= 0) return false;
  return scroller.scrollHeight - scroller.clientHeight - scroller.scrollTop <= 4;
}

function currentSection(sections: readonly HTMLElement[], line: number): string | undefined {
  const visible = sections.filter((section) => section.getClientRects().length > 0);
  // A short final section cannot always reach the activation line.
  if (atDocumentBottom()) return visible.at(-1)?.id;
  return visible.findLast((section) => section.getBoundingClientRect().top <= line)?.id ?? visible[0]?.id;
}

function markCurrent(links: ReadonlyMap<string, HTMLElement>, current: string | undefined, activeClass: string): void {
  links.forEach((link, id) => {
    const active = id === current;
    link.classList.toggle(activeClass, active);
    if (active) link.setAttribute('aria-current', 'location');
    else link.removeAttribute('aria-current');
  });
}

function menuScroller(root: HTMLElement | undefined): HTMLElement | undefined {
  for (let container = root; container; container = container.parentElement ?? undefined) {
    if (container === document.body || container === document.documentElement) return undefined;
    if (container.scrollHeight <= container.clientHeight) continue;
    if (/auto|scroll/.test(window.getComputedStyle(container).overflowY)) return container;
  }
  return undefined;
}

function revealLink(link: HTMLElement, container: HTMLElement): void {
  const bounds = container.getBoundingClientRect();
  const linkBounds = link.getBoundingClientRect();
  // Scroll the menu only; scrollIntoView would also move the document.
  if (linkBounds.top < bounds.top) container.scrollTop += linkBounds.top - bounds.top;
  else if (linkBounds.bottom > bounds.bottom) container.scrollTop += linkBounds.bottom - bounds.bottom;
}

/** Track the last section above a viewport line, with the final section
 * taking priority at the bottom. Never changes the URL or moves page focus. */
export function mountScrollSpy(
  sections: readonly HTMLElement[],
  links: ReadonlyMap<string, HTMLElement>,
  options: { threshold?: number; offset?: () => number; activeClass?: string; scrollContainer?: HTMLElement } = {},
): () => void {
  const { threshold = 0.35, offset, activeClass = 'is-current', scrollContainer } = options;
  let frame: number | undefined;
  let disposed = false;
  let previous: string | undefined;
  let menuHeight: number | undefined;
  function update() {
    frame = undefined;
    if (disposed) return;
    const current = currentSection(sections, offset?.() ?? window.innerHeight * threshold);
    markCurrent(links, current, activeClass);
    const activeLink = current ? links.get(current) : undefined;
    const container = menuScroller(scrollContainer);
    if (activeLink && container) {
      if (current !== previous || menuHeight !== container.clientHeight) revealLink(activeLink, container);
      menuHeight = container.clientHeight;
    }
    previous = current;
  }
  function schedule() {
    if (frame === undefined) frame = requestAnimationFrame(update);
  }
  function resize() {
    menuHeight = undefined;
    schedule();
  }
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', resize, { passive: true });
  window.addEventListener('hashchange', schedule);
  const observer = new ResizeObserver(resize);
  observer.observe(document.documentElement);
  sections.forEach((section) => observer.observe(section));
  for (let ancestor = scrollContainer; ancestor && ancestor !== document.body; ancestor = ancestor.parentElement ?? undefined) {
    observer.observe(ancestor);
  }
  update();
  return () => {
    disposed = true;
    window.removeEventListener('scroll', schedule);
    window.removeEventListener('resize', resize);
    window.removeEventListener('hashchange', schedule);
    observer.disconnect();
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
  };
}

/** Bind a section menu to its current targets. Rebind when filtering replaces
 * links, so removed sections and stale URL fragments cannot keep an item lit. */
export function mountSectionNav(root: HTMLElement): () => void {
  let dispose: (() => void) | undefined;
  function bind() {
    dispose?.();
    const sections: HTMLElement[] = [];
    const links = new Map<string, HTMLElement>();
    for (const link of root.querySelectorAll<HTMLAnchorElement>('a[href^="#"]')) {
      link.classList.remove('is-active');
      link.removeAttribute('aria-current');
      let id: string;
      try { id = decodeURIComponent(link.hash.slice(1)); }
      catch { continue; }
      const section = document.getElementById(id);
      if (!section) continue;
      sections.push(section);
      links.set(id, link);
    }
    dispose = mountScrollSpy(sections, links, {
      activeClass: 'is-active',
      scrollContainer: root,
      // Match native anchor landing positions below the page's sticky chrome.
      offset: () => Math.max(0, ...sections.map((section) =>
        parseFloat(window.getComputedStyle(section).scrollMarginTop) || 0)) + 1,
    });
  }
  bind();
  const observer = new MutationObserver(bind);
  observer.observe(root, { childList: true, subtree: true, attributes: true, attributeFilter: ['href'] });
  return () => {
    observer.disconnect();
    dispose?.();
  };
}
