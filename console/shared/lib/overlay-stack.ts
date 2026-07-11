// Shared overlay stack + focus management for every modal surface (Modal,
// ConfirmDialog, Drawer, and the coming mobile Inspector). Fixes the decentralised
// behaviour the audit flagged: each overlay owned its own `document.body.overflow`
// write (nested overlays fought over it), every open overlay listened for Escape
// on window (one keypress closed two surfaces), dialog semantics sat on the
// backdrop, and focus was never trapped or restored.
//
// Model: while open, a surface calls pushOverlay() and portals its root to
// <body>. The stack ref-counts the scroll lock and marks the rest of the page
// `inert`, computes z-order, and answers isTopmost() so only the frontmost
// surface reacts to Escape. Framework-free; components wire their lifecycle to it.

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

// Only the frontmost overlay should act on Escape / backdrop dismissal.
export function isTopmost(id: number): boolean {
  return stack.length > 0 && stack[stack.length - 1] === id;
}

// Stacking position at open time; callers turn it into a z-index so a
// confirmation always renders above the surface that spawned it.
export function overlayIndex(id: number): number {
  const i = stack.indexOf(id);
  return i < 0 ? 0 : i;
}

// --- Scroll lock + background inert, reference-counted across the whole stack ---
let locked = false;
let prevOverflow = '';
const inerted: Element[] = [];

function applyLock(): void {
  if (typeof document === 'undefined') return;
  const shouldLock = stack.length > 0;
  if (shouldLock && !locked) {
    prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const lenis = (window as unknown as { __lenis?: { stop(): void } }).__lenis;
    lenis?.stop();
    // Everything that is not an overlay portal becomes inert, so assistive tech
    // and Tab cannot reach the page behind the stack.
    for (const child of Array.from(document.body.children)) {
      if (child.hasAttribute('data-overlay')) continue;
      child.setAttribute('inert', '');
      inerted.push(child);
    }
    locked = true;
  } else if (!shouldLock && locked) {
    document.body.style.overflow = prevOverflow;
    const lenis = (window as unknown as { __lenis?: { start(): void } }).__lenis;
    lenis?.start();
    for (const el of inerted) el.removeAttribute('inert');
    inerted.length = 0;
    locked = false;
  }
}

// portal moves a node to <body> so fixed-position overlays escape any clipping /
// transformed ancestor and the inert sweep above can exclude them. All design
// tokens live on :root, so a body child still inherits the full theme.
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

// trapFocus: initial focus into the surface, Tab/Shift+Tab kept inside it, and
// focus restored to the opener on close. Mounted only while the surface is open
// (inside its {#if open}), so destroy == close.
export function trapFocus(node: HTMLElement) {
  const opener = document.activeElement as HTMLElement | null;

  const visible = (el: HTMLElement) => el.offsetParent !== null || el === document.activeElement;
  const items = () => Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(visible);

  // Focus after the current mount flush: the surface is portaled to <body> and
  // laid out by the next frame, so offsetParent is set and moving the node no
  // longer blurs the focus we just placed.
  const raf = requestAnimationFrame(() => {
    const first = items()[0];
    if (first) first.focus();
    else node.focus();
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
      node.removeEventListener('keydown', onKeydown);
      if (opener && document.contains(opener)) opener.focus();
    }
  };
}
