// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export function mountScrollSpy(
  sections: readonly HTMLElement[],
  links: ReadonlyMap<string, HTMLElement>,
  options: { threshold?: number; activeClass?: string } = {},
): () => void {
  const { threshold = 0.35, activeClass = 'is-current' } = options;
  let frame: number | undefined;
  let disposed = false;
  function update() {
    frame = undefined;
    if (disposed) return;
    let current = sections[0]?.id;
    for (const section of sections) {
      if (section.getBoundingClientRect().top < window.innerHeight * threshold) current = section.id;
    }
    links.forEach((link, id) => link.classList.toggle(activeClass, id === current));
  }
  function schedule() {
    if (frame === undefined) frame = requestAnimationFrame(update);
  }
  window.addEventListener('scroll', schedule, { passive: true });
  window.addEventListener('resize', schedule, { passive: true });
  update();
  return () => {
    disposed = true;
    window.removeEventListener('scroll', schedule);
    window.removeEventListener('resize', schedule);
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
  };
}
