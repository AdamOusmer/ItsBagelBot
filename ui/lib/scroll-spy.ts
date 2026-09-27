// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
    const visible = sections.filter((section) => section.getClientRects().length > 0);
    let current: string | undefined = visible[0]?.id;
    const line = offset?.() ?? window.innerHeight * threshold;
    for (const section of visible) {
      if (section.getBoundingClientRect().top <= line) current = section.id;
    }
    const scroller = document.scrollingElement;
    // A short last section cannot reach the activation line. Allow fractional
    // scroll positions, but do not select the last item on an unscrolled page.
    if (scroller && scroller.scrollTop > 0 &&
        scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4) {
      current = visible.at(-1)?.id;
    }
    links.forEach((link, id) => {
      const active = id === current;
      link.classList.toggle(activeClass, active);
      if (active) link.setAttribute('aria-current', 'location');
      else link.removeAttribute('aria-current');
    });
    const activeLink = current ? links.get(current) : undefined;
    let container = scrollContainer;
    while (activeLink && container && container !== document.body && container !== document.documentElement) {
      if (container.scrollHeight > container.clientHeight &&
          /auto|scroll/.test(window.getComputedStyle(container).overflowY)) {
        if (current !== previous || menuHeight !== container.clientHeight) {
          const bounds = container.getBoundingClientRect();
          const linkBounds = activeLink.getBoundingClientRect();
          // Reveal a changed selection without fighting someone scrolling
          // the menu to another link, or scrolling the document itself.
          if (linkBounds.top < bounds.top) container.scrollTop += linkBounds.top - bounds.top;
          else if (linkBounds.bottom > bounds.bottom) container.scrollTop += linkBounds.bottom - bounds.bottom;
        }
        menuHeight = container.clientHeight;
        break;
      }
      container = container.parentElement ?? undefined;
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
