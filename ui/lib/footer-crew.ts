// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { EngineHandle } from '@luzir/bolota/engine';

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
const MOUNT_WAIT_MS = 3000;

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

  let stopped = false;

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

  async function mount(): Promise<void> {
    const { mountEngine } = await import('@luzir/bolota/engine');
    for (const seat of crew) {
      await idle();
      if (stopped) return;
      seat.handle = mountEngine(seat.svg, seat.name, { size: 100, palette: seat.palette, background: false });
      seat.handle.loop('idle');
      seat.handle.follow('window');
    }
  }

  function hide(): void {
    window.removeEventListener('pointermove', lift);
    settle();
  }

  void idle(MOUNT_WAIT_MS).then(mount);

  const io = new IntersectionObserver(
    ([entry]) => (entry.isIntersecting ? window.addEventListener('pointermove', lift, { passive: true }) : hide()),
    { rootMargin: '200px 0px' },
  );
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
    }
  };
}
