// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The marketing site's top-down mobile menu (web/marketing/src/components/layout/Nav.astro,
// TopDownMenuAnimator), ported for the public pages' nav. Three motions run
// together on open: the panel slides down from above, a curved clip-path
// lifts its bottom edge from a shallow arc to the full viewport, and the
// links drop in one after another with a small overshoot. Close runs each in
// reverse. Progress is a 0..1 number the callers never see: the class owns
// the DOM writes so the Svelte side only flips `open`.
import { animate, cubicBezier } from 'motion';

type Stoppable = { stop: () => void };

/** How far the arc sags below the panel's bottom edge while closed. */
const CURVE_DEPTH = 220;
/** Panel height, in px, the arc starts from before it grows to the viewport. */
const INITIAL_HEIGHT = 96;
/** The first link's resting offset above its slot; later ones sit higher. */
const INITIAL_OFFSET = -34;
const OFFSET_STEP = -8;

const PANEL_EASE = cubicBezier(0.33, 1, 0.68, 1);
const CURVE_EASE = cubicBezier(0.25, 0.1, 0.25, 1);
const ITEM_EASE = cubicBezier(0.34, 1.56, 0.64, 1);

export class MenuAnimator {
  private readonly items: HTMLElement[];
  private active: Stoppable[] = [];
  private open = false;

  constructor(
    private readonly menu: HTMLElement,
    private readonly path: SVGPathElement
  ) {
    this.items = Array.from(menu.querySelectorAll<HTMLElement>('[data-menu-item], [data-menu-footer]'));
    this.renderAll(0);
  }

  /** Open or close; `instant` snaps without motion (viewport flips, pagehide). */
  set(open: boolean, instant = false): void {
    if (open === this.open && !instant) return;
    this.stop();
    this.open = open;

    if (open) this.menu.classList.add('is-visible', 'is-open');
    else this.menu.classList.remove('is-open');

    if (instant) {
      this.renderAll(open ? 1 : 0);
      if (!open) this.menu.classList.remove('is-visible');
      return;
    }
    this.animate(open);
  }

  /** The clip path is sized from the menu box, so a resize redraws it. */
  resize(): void {
    this.renderPath(this.open ? 1 : 0);
  }

  destroy(): void {
    this.stop();
  }

  private animate(open: boolean): void {
    const from = open ? 0 : 1;
    const to = open ? 1 : 0;

    this.active.push(
      animate(from, to, {
        duration: 0.7,
        ease: PANEL_EASE,
        onUpdate: (v) => this.renderMenu(v),
        onComplete: () => {
          this.renderMenu(to);
          if (!open) this.menu.classList.remove('is-visible');
        }
      })
    );
    this.active.push(
      animate(from, to, {
        duration: 0.95,
        ease: CURVE_EASE,
        onUpdate: (v) => this.renderPath(v),
        onComplete: () => this.renderPath(to)
      })
    );
    this.items.forEach((item, i) => this.animateItem(item, i, open));
  }

  private animateItem(item: HTMLElement, index: number, open: boolean): void {
    const from = open ? 0 : 1;
    const to = open ? 1 : 0;
    const delay = open ? 0.1 + index * 0.07 : (this.items.length - 1 - index) * 0.035;
    this.active.push(
      animate(from, to, {
        duration: 0.72,
        delay,
        ease: ITEM_EASE,
        onUpdate: (v) => this.renderItem(item, index, v),
        onComplete: () => this.renderItem(item, index, to)
      })
    );
  }

  private renderAll(progress: number): void {
    this.renderMenu(progress);
    this.renderPath(progress);
    this.items.forEach((item, i) => this.renderItem(item, i, progress));
  }

  private renderItem(item: HTMLElement, index: number, progress: number): void {
    const offset = (INITIAL_OFFSET + OFFSET_STEP * index) * (1 - progress);
    item.style.opacity = String(progress);
    item.style.transform = `translateY(${offset}px)`;
  }

  private renderMenu(progress: number): void {
    this.menu.style.transform = `translateY(${(1 - progress) * -100}%)`;
  }

  private renderPath(progress: number): void {
    const { width, height } = this.menu.getBoundingClientRect();
    const w = Math.max(1, width || window.innerWidth);
    const fullH = Math.max(1, height || window.innerHeight);
    const h = INITIAL_HEIGHT + (fullH - INITIAL_HEIGHT) * progress;
    const curve = CURVE_DEPTH * (1 - progress);
    this.path.setAttribute('d', `M 0 0 L ${w} 0 L ${w} ${h} Q ${w / 2} ${h + curve}, 0 ${h} L 0 0 Z`);
  }

  private stop(): void {
    for (const a of this.active) a.stop();
    this.active = [];
  }
}
