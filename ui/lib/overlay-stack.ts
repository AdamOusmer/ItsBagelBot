// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

let seq = 0;
const stack: number[] = [];

export function pushOverlay(): number {
  const id = ++seq;
  stack.push(id);
  applyLock();
  return id;
}

export function removeOverlay(id: number): void {
  const i = stack.indexOf(id);
  if (i >= 0) stack.splice(i, 1);
  applyLock();
}

export function isTopmost(id: number): boolean {
  return stack.length > 0 && stack[stack.length - 1] === id;
}

export function overlayIndex(id: number): number {
  const i = stack.indexOf(id);
  return i < 0 ? 0 : i;
}

export function hasOpenOverlay(): boolean {
  return stack.length > 0;
}

let locked = false;
let prevOverflow = '';
const inerted: Element[] = [];

function acquireLock(): void {
  prevOverflow = document.body.style.overflow;
  document.body.style.overflow = 'hidden';
  (window as unknown as { __lenis?: { stop(): void } }).__lenis?.stop();
  for (const child of Array.from(document.body.children)) {
    if (child.hasAttribute('data-overlay')) continue;
    child.setAttribute('inert', '');
    inerted.push(child);
  }
  locked = true;
}

function releaseLock(): void {
  document.body.style.overflow = prevOverflow;
  (window as unknown as { __lenis?: { start(): void } }).__lenis?.start();
  for (const el of inerted) el.removeAttribute('inert');
  inerted.length = 0;
  locked = false;
}

function applyLock(): void {
  if (typeof document === 'undefined') return;
  const shouldLock = stack.length > 0;
  if (shouldLock && !locked) acquireLock();
  else if (!shouldLock && locked) releaseLock();
}

export function portal(node: HTMLElement, target: HTMLElement = document.body) {
  target.appendChild(node);
  return {
    destroy() {
      node.parentNode?.removeChild(node);
    }
  };
}

const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function trapFocus(node: HTMLElement) {
  const opener = document.activeElement as HTMLElement | null;

  const visible = (el: HTMLElement) => el.offsetParent !== null || el === document.activeElement;
  const items = () => Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(visible);

  let raf2 = 0;
  const raf = requestAnimationFrame(() => {
    raf2 = requestAnimationFrame(() => {
      const first = items()[0];
      if (first) first.focus();
      else node.focus();
    });
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Tab') return;
    const list = items();
    if (list.length === 0) {
      e.preventDefault();
      return;
    }
    const head = list[0];
    const tail = list[list.length - 1];
    const active = document.activeElement as HTMLElement;
    if (e.shiftKey && (active === head || !node.contains(active))) {
      e.preventDefault();
      tail.focus();
    } else if (!e.shiftKey && (active === tail || !node.contains(active))) {
      e.preventDefault();
      head.focus();
    }
  }
  node.addEventListener('keydown', onKeydown);

  return {
    destroy() {
      cancelAnimationFrame(raf);
      cancelAnimationFrame(raf2);
      node.removeEventListener('keydown', onKeydown);
      if (opener && document.contains(opener)) opener.focus();
    }
  };
}
