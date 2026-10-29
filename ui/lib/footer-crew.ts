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

  let painted = false;
  let live = false;
  let stopped = false;

  const lift = (ev: PointerEvent) => {
    const close = ev.clientY > footer.getBoundingClientRect().bottom - LIFT_ZONE_PX;
    for (const seat of crew) {
      const box = seat.el.getBoundingClientRect();
      const amount = close ? Math.max(0, 1 - Math.abs(ev.clientX - (box.left + box.width / 2)) / LIFT_REACH_PX) : 0;
      seat.el.style.setProperty('--lift', amount.toFixed(2));
      seat.handle?.setExpression(amount > 0.6 ? 'curious' : null);
    }
  };
  const settle = () => crew.forEach((seat) => seat.el.style.setProperty('--lift', '0'));

  async function paint(): Promise<void> {
    if (painted) return;
    painted = true;
    const { parts } = await import('@luzir/bolota');
    for (const seat of crew) {
      seat.svg.innerHTML = `<g data-pose>${parts(seat.name, { size: 100, palette: seat.palette, background: false }).inner}</g>`;
    }
  }

  async function wake(): Promise<void> {
    await paint();
    if (live || stopped || prefersReducedMotion()) return;
    live = true;
    const { mountEngine } = await import('@luzir/bolota/engine');
    if (!live || stopped) return;
    for (const seat of crew) {
      seat.handle = mountEngine(seat.svg, seat.name, { size: 100, palette: seat.palette, background: false });
      seat.svg.querySelector('[data-pose]')?.setAttribute('display', 'none');
      seat.handle.loop('idle');
      seat.handle.follow('window');
    }
    window.addEventListener('pointermove', lift, { passive: true });
  }

  function sleep(): void {
    live = false;
    window.removeEventListener('pointermove', lift);
    settle();
    for (const seat of crew) {
      seat.handle?.destroy();
      seat.handle = undefined;
      seat.svg.querySelector('[data-pose]')?.removeAttribute('display');
    }
  }

  const io = new IntersectionObserver(([entry]) => (entry.isIntersecting ? void wake() : sleep()), {
    rootMargin: '200px 0px',
  });
  io.observe(footer);
  footer.addEventListener('pointerleave', settle);

  return () => {
    stopped = true;
    io.disconnect();
    footer.removeEventListener('pointerleave', settle);
    sleep();
  };
}
