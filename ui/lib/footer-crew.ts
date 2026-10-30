// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { EngineHandle } from '@luzir/bolota/engine';
import { prefersReducedMotion } from './motion-query';

export const FOOTER_CREW = [
  { name: 'everything', head: '--bb-green-glow', size: 0.82 },
  { name: 'sesame', head: '--bb-tan-light', size: 1 },
  { name: 'poppy', head: '--bb-mint', size: 0.74 },
  { name: 'plain', head: '--bb-tan-pale', size: 0.92 },
  { name: 'cinnamon', head: '--bb-tan', size: 0.86 },
] as const;

const LIFT_REACH_PX = 220;
const LIFT_ZONE_PX = 360;
const IDLE_WAIT_MS = 200;
const SCROLL_SETTLE_MS = 150;
const WARM_WAIT_MS = 3000;

function idle(timeout = IDLE_WAIT_MS): Promise<void> {
  return new Promise((resolve) => {
    if ('requestIdleCallback' in window) requestIdleCallback(() => resolve(), { timeout });
    else setTimeout(resolve, 0);
  });
}

type Seat = {
  el: HTMLElement;
  svg: SVGSVGElement;
  name: string;
  palette: { head: string; eye: string };
  handle?: EngineHandle;
};

function seats(footer: HTMLElement): Seat[] {
  const styles = getComputedStyle(document.documentElement);
  const eye = styles.getPropertyValue('--bb-black').trim();
  return [...footer.querySelectorAll<HTMLElement>('[data-crew]')].map((el) => ({
    el,
    svg: el.querySelector('svg')!,
    name: el.dataset.crew!,
    palette: { head: styles.getPropertyValue(el.dataset.head!).trim(), eye },
  }));
}

export function mountFooterCrew(footer: HTMLElement): () => void {
  const crew = seats(footer);
  if (!crew.length) return () => {};

  const animate = !prefersReducedMotion();
  let visible = false;
  let stopped = false;
  let scrolling: ReturnType<typeof setTimeout> | undefined;
  let following = true;

  // Following reads each bolota's rect every frame; during scroll that forces a layout per bolota per frame.
  const syncGaze = () => {
    const on = scrolling === undefined;
    if (on === following) return;
    following = on;
    crew.forEach((seat) => seat.handle?.follow(on ? 'window' : false));
  };
  const hold = () => {
    clearTimeout(scrolling);
    scrolling = setTimeout(() => {
      scrolling = undefined;
      syncGaze();
    }, SCROLL_SETTLE_MS);
    syncGaze();
  };

  const lift = (ev: PointerEvent) => {
    const close = ev.clientY > footer.getBoundingClientRect().bottom - LIFT_ZONE_PX;
    const centres = crew.map((seat) => {
      const box = seat.el.getBoundingClientRect();
      return box.left + box.width / 2;
    });
    crew.forEach((seat, i) => {
      const amount = close ? Math.max(0, 1 - Math.abs(ev.clientX - centres[i]) / LIFT_REACH_PX) : 0;
      seat.el.style.setProperty('--lift', amount.toFixed(2));
      seat.handle?.setExpression(amount > 0.6 ? 'curious' : null);
    });
  };
  const settle = () => crew.forEach((seat) => seat.el.style.setProperty('--lift', '0'));

  const wakeSeat = (seat: Seat) => {
    seat.handle?.loop('idle');
    seat.handle?.follow(following ? 'window' : false);
  };

  // The engines share one frame loop and reuse their nodes, so each is mounted once at idle and only paused off-screen.
  async function prepare(): Promise<void> {
    const { parts } = await import('@luzir/bolota');
    if (stopped) return;
    for (const seat of crew) {
      seat.svg.innerHTML = `<g data-pose>${parts(seat.name, { size: 100, palette: seat.palette, background: false }).inner}</g>`;
    }
    if (!animate) return;
    const { mountEngine } = await import('@luzir/bolota/engine');
    for (const seat of crew) {
      await idle();
      if (stopped) return;
      seat.handle = mountEngine(seat.svg, seat.name, { size: 100, palette: seat.palette, background: false });
      seat.svg.querySelector('[data-pose]')?.setAttribute('display', 'none');
      if (visible) wakeSeat(seat);
      else seat.handle.stop();
    }
  }

  function show(): void {
    visible = true;
    window.addEventListener('pointermove', lift, { passive: true });
    window.addEventListener('scroll', hold, { passive: true });
    crew.forEach(wakeSeat);
  }

  function hide(): void {
    visible = false;
    window.removeEventListener('pointermove', lift);
    window.removeEventListener('scroll', hold);
    clearTimeout(scrolling);
    scrolling = undefined;
    following = true;
    settle();
    crew.forEach((seat) => seat.handle?.stop());
  }

  void idle(WARM_WAIT_MS).then(prepare);

  const io = new IntersectionObserver(([entry]) => (entry.isIntersecting ? show() : hide()), {
    rootMargin: '200px 0px',
  });
  io.observe(footer);
  footer.addEventListener('pointerleave', settle);

  return () => {
    stopped = true;
    io.disconnect();
    footer.removeEventListener('pointerleave', settle);
    hide();
    for (const seat of crew) {
      seat.handle?.destroy();
      seat.handle = undefined;
      seat.svg.querySelector('[data-pose]')?.removeAttribute('display');
    }
  };
}
