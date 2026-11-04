<script lang="ts" module>
  import type { Dir } from '../../flow/keyboard';

  export type FlowTrace = { nodes: ReadonlySet<string>; edges: ReadonlySet<string>; current: string | null };

  export type FlowCanvasApi = {
    tidyNow: () => void;
    fitView: (reservedRight?: number) => void;
    announce: (message: string) => void;
    focusNode: (id: string) => void;
    walk: (dir: Dir) => boolean;
    nudge: (dir: Dir) => boolean;
    removeSelected: () => boolean;
    duplicateSelected: () => boolean;
    openAddMenu: (other?: boolean) => boolean;
    dismissMenu: () => boolean;
    zoomBy: (factor: number) => void;
    settle: (retidy: boolean) => void;
  };
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick, untrack } from 'svelte';
  import Icon from './FlowIcon.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Cluster from '@bagel/ui/svelte/Cluster.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import VisuallyHidden from '@bagel/ui/svelte/VisuallyHidden.svelte';
  import FlowNodeCard from './FlowNode.svelte';
  import FlowStart, { START_H, START_W } from './FlowStart.svelte';
  import { GRID, NODE_SPECS, NODE_W, outputsFor, specFor, type Flow, type FlowKind } from '../../flow/model';
  import { addNode, connect, duplicateNode, removeEdge, removeNode } from '../../flow/ops';
  import { neighbor, neighborAfterRemove, readingOrder, stepDelta, stepFocus } from '../../flow/keyboard';
  import type { Move } from '../../flow/history';
  import { describe } from '../../flow/describe';
  import { inputY, nodeHeight, portY, tidy } from '../../flow/layout';
  import { wirePath } from '../../flow/wire';
  import { beginPinch, fitBox, pinchView, revealBox, type Inset, type Pinch } from '../../flow/touch';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import { getI18n } from '@bagel/kit';

  let {
    flow = $bindable(),
    selectedId = $bindable<string | null>(null),
    autoTidy = $bindable(true),
    trace = null,
    onstart,
    insets,
    room = 0,
    onmoved,
    oninspect
  }: {
    flow: Flow;
    selectedId?: string | null;
    autoTidy?: boolean;
    trace?: FlowTrace | null;
    onstart?: (byKey: boolean) => void;
    insets?: (withPanel: boolean) => Inset | null;
    room?: number;
    onmoved?: (moves: Move[]) => void;
    oninspect?: (id: string) => void;
  } = $props();

  const { t } = getI18n();

  const startAt: Pt = { x: -START_W - 56, y: -24 };

  type Pt = { x: number; y: number };
  const MIN_ZOOM = 0.4;
  const MAX_ZOOM = 1.6;
  const TIDY_MS = 320;
  const FIT_INSET = { top: 160, right: 48, bottom: 96, left: 120 };
  const KEY_INSET = { top: 104, right: 24, bottom: 80, left: 112 };
  const DRAG_SLOP = 3;
  const TOUCH_SLOP = 8;
  const OWN_TAPS = 'button, [data-flow-ui]:not([data-pan-through])';
  const TOUCH_MIN_ZOOM = 0.25;
  const READABLE_ZOOM = 0.75;
  const ADD_ROOM = 48;
  const GLIDE_MS = 240;
  const ARROW_GAP = 8;
  const MENU_KINDS: { kind: FlowKind; title: string }[] = [
    { kind: 'condition', title: 'Only if' },
    { kind: 'reply', title: 'Do' }
  ];
  const MENU_TINT: Record<FlowKind, { color: string; rgb: string }> = {
    trigger: { color: 'var(--bb-green-glow)', rgb: 'var(--bb-green-glow-rgb)' },
    condition: { color: 'var(--bb-tan)', rgb: 'var(--bb-tan-rgb)' },
    reply: { color: 'var(--bb-muted-light)', rgb: 'var(--bb-muted-rgb)' }
  };

  let host = $state<HTMLDivElement>();
  let view = $state({ x: 0, y: 0, k: 1 });
  let disp = $state<Record<string, Pt>>({});
  let dragging = $state(false);
  let wire = $state<{ from: string; port: string; x: number; y: number } | null>(null);
  let hoverEdge = $state<string | null>(null);
  let menu = $state<{ link?: { from: string; port: string }; at: Pt; sx: number; sy: number } | null>(null);
  let menuAnchor = $state<HTMLElement>();
  let menuOpen = $state(false);
  let query = $state('');
  let touch = $state(false);
  let pinchK = $state<number | null>(null);
  let connecting = $state<{ from: string; port: string } | null>(null);
  let viewOpen = $state(false);
  let note = $state('');
  let menuByKey = $state(false);
  let returnFocus: HTMLElement | null = null;

  let mode: 'idle' | 'pan' | 'node' | 'wire' | 'pinch' = 'idle';
  let start = { px: 0, py: 0, vx: 0, vy: 0, nx: 0, ny: 0, moved: false };
  let activeNode: string | null = null;
  let tween = 0;
  let tweenTimer: ReturnType<typeof setTimeout> | undefined;
  let hoverTimer: ReturnType<typeof setTimeout> | undefined;
  let glide = 0;
  const fingers = new Map<number, Pt>();
  let pinch: Pinch | null = null;
  let tapped: { port?: string; node?: string; edge?: string } = {};

  const minZoom = () => (touch ? TOUCH_MIN_ZOOM : MIN_ZOOM);
  const shownK = $derived(pinchK ?? view.k);

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  const nodeById = (id: string) => flow.nodes.find((n) => n.id === id);
  const at = (id: string): Pt => disp[id] ?? nodeById(id) ?? { x: 0, y: 0 };
  const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));
  const snap = (v: number) => Math.round(v / GRID) * GRID;

  function toWorld(clientX: number, clientY: number): Pt {
    const r = (host as HTMLElement).getBoundingClientRect();
    return { x: (clientX - r.left - view.x) / view.k, y: (clientY - r.top - view.y) / view.k };
  }

  function finishTween() {
    cancelAnimationFrame(tween);
    clearTimeout(tweenTimer);
    for (const n of flow.nodes) disp[n.id] = { x: n.x, y: n.y };
  }

  function applyPositions(targets: Map<string, Pt>, animate: boolean) {
    finishTween();
    const from = new Map(flow.nodes.map((n) => [n.id, { x: disp[n.id]?.x ?? n.x, y: disp[n.id]?.y ?? n.y }]));
    for (const n of flow.nodes) {
      const t = targets.get(n.id);
      if (t) {
        n.x = t.x;
        n.y = t.y;
      }
    }
    if (!animate || reduced()) {
      for (const n of flow.nodes) disp[n.id] = { x: n.x, y: n.y };
      return;
    }
    const t0 = performance.now();
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / TIDY_MS);
      const e = p === 1 ? 1 : 1 - Math.pow(2, -10 * p);
      for (const n of flow.nodes) {
        const a = from.get(n.id) as Pt;
        disp[n.id] = { x: a.x + (n.x - a.x) * e, y: a.y + (n.y - a.y) * e };
      }
      if (p < 1) tween = requestAnimationFrame(step);
    };
    tween = requestAnimationFrame(step);
    tweenTimer = setTimeout(finishTween, TIDY_MS + 120);
  }

  function runTidy(animate: boolean) {
    applyPositions(tidy(flow), animate);
    revealSelected();
  }

  function glideTo(target: typeof view) {
    cancelAnimationFrame(glide);
    if (reduced()) {
      view = target;
      return;
    }
    const a = view;
    const t0 = performance.now();
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / GLIDE_MS);
      const e = p === 1 ? 1 : 1 - Math.pow(2, -10 * p);
      view = { k: a.k + (target.k - a.k) * e, x: a.x + (target.x - a.x) * e, y: a.y + (target.y - a.y) * e };
      if (p < 1) glide = requestAnimationFrame(step);
    };
    glide = requestAnimationFrame(step);
  }

  function revealSelected() {
    const n = selectedId ? nodeById(selectedId) : undefined;
    const room = insets?.(true);
    if (!n || !room || !host) return;
    const box = { x0: n.x, y0: n.y, x1: n.x + NODE_W + ADD_ROOM, y1: n.y + nodeHeight(specFor(n.type)) };
    const target = revealBox(view, box, { width: host.clientWidth, height: host.clientHeight, inset: room }, READABLE_ZOOM);
    if (target !== view) glideTo(target);
  }

  export function tidyNow() {
    runTidy(true);
  }

  export function fitView(reservedRight = 0) {
    if (!host) return;
    let x0 = startAt.x;
    let y0 = startAt.y;
    let x1 = startAt.x + START_W;
    let y1 = startAt.y + START_H;
    for (const n of flow.nodes) {
      x0 = Math.min(x0, n.x);
      y0 = Math.min(y0, n.y);
      x1 = Math.max(x1, n.x + NODE_W);
      y1 = Math.max(y1, n.y + nodeHeight(specFor(n.type)));
    }
    const inset = insets?.(reservedRight > 0) ?? { ...FIT_INSET, right: FIT_INSET.right + reservedRight };
    cancelAnimationFrame(glide);
    view = fitBox({ x0, y0, x1, y1 }, { width: host.clientWidth, height: host.clientHeight, inset }, { min: minZoom(), max: MAX_ZOOM });
  }

  function zoomAt(factor: number, px: number, py: number) {
    const k = clamp(view.k * factor, minZoom(), MAX_ZOOM);
    const wx = (px - view.x) / view.k;
    const wy = (py - view.y) / view.k;
    view = { k, x: px - wx * k, y: py - wy * k };
  }

  function zoomCenter(factor: number) {
    if (host) zoomAt(factor, host.clientWidth / 2, host.clientHeight / 2);
  }

  function onWheel(e: WheelEvent) {
    e.preventDefault();
    const r = (host as HTMLElement).getBoundingClientRect();
    if (e.ctrlKey || e.metaKey) zoomAt(Math.exp(-e.deltaY * 0.01), e.clientX - r.left, e.clientY - r.top);
    else view = { ...view, x: view.x - e.deltaX, y: view.y - e.deltaY };
  }

  $effect(() => {
    const id = selectedId;
    const reserved = room;
    untrack(() => {
      const n = id ? flow.nodes.find((m) => m.id === id) : undefined;
      if (!n || !host) return;
      if (insets?.(true)) {
        revealSelected();
        return;
      }
      if (reserved <= 0) return;
      const right = view.x + (n.x + NODE_W) * view.k;
      const limit = host.clientWidth - reserved;
      if (right > limit) view = { ...view, x: view.x - (right - limit) };
    });
  });

  const stopGesture = (e: Event) => e.preventDefault();
  // Tap logic runs on pointerup. A follow-up click would land on the + a just selected step reveals, and its focus would reselect.
  const stopCompatClick = (e: TouchEvent) => {
    if (!(e.target as Element).closest(OWN_TAPS)) e.preventDefault();
  };

  onMount(() => {
    touch = matchMedia('(pointer: coarse)').matches;
    runTidy(false);
    fitView();
    host?.addEventListener('wheel', onWheel, { passive: false });
    host?.addEventListener('gesturestart', stopGesture);
    host?.addEventListener('touchend', stopCompatClick, { passive: false });
    return () => {
      host?.removeEventListener('wheel', onWheel);
      host?.removeEventListener('gesturestart', stopGesture);
      host?.removeEventListener('touchend', stopCompatClick);
      cancelAnimationFrame(tween);
      cancelAnimationFrame(glide);
      clearTimeout(tweenTimer);
      clearTimeout(hoverTimer);
    };
  });

  $effect(() => {
    for (const n of flow.nodes) if (!disp[n.id]) disp[n.id] = { x: n.x, y: n.y };
  });

  function onTouchDown(e: PointerEvent) {
    const aimed = e.target as HTMLElement;
    viewOpen = viewOpen && Boolean(aimed.closest('.zoom, .view-toggle'));
    if (aimed.closest(OWN_TAPS)) return;
    // Browsers retarget a touch to a nearby clickable, which would steal taps on wires next to a card.
    const el = (document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null) ?? aimed;
    (host as HTMLElement).setPointerCapture(e.pointerId);
    fingers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    cancelAnimationFrame(glide);
    closeMenu();
    if (fingers.size > 1) {
      startPinch();
      return;
    }
    start = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y, nx: 0, ny: 0, moved: false };
    const port = el.closest<HTMLElement>('[data-port]');
    const id = el.closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
    const n = id ? nodeById(id) : undefined;
    tapped = { port: port?.dataset.port, node: id, edge: el.closest<SVGElement>('[data-edge]')?.dataset.edge };
    if (port && id && !connecting) {
      const p = toWorld(e.clientX, e.clientY);
      wire = { from: id, port: port.dataset.port as string, x: p.x, y: p.y };
      mode = 'wire';
    } else if (n && !connecting) {
      finishTween();
      activeNode = n.id;
      start.nx = n.x;
      start.ny = n.y;
      mode = 'node';
    } else {
      mode = 'pan';
    }
  }

  function undoSingleFinger() {
    const n = activeNode ? nodeById(activeNode) : undefined;
    if (n && start.moved) {
      n.x = start.nx;
      n.y = start.ny;
      disp[n.id] = { x: n.x, y: n.y };
    }
    dragging = false;
    activeNode = null;
    wire = null;
  }

  function startPinch() {
    undoSingleFinger();
    const [a, b] = fingers.values();
    pinch = beginPinch(view, a, b);
    pinchK = view.k;
    mode = 'pinch';
  }

  function tap() {
    (document.activeElement as HTMLElement | null)?.blur?.();
    const { port, node, edge } = tapped;
    if (connecting) {
      if (node && targetFor(node)) {
        const made = connect(flow, { from: connecting.from, port: connecting.port, to: node });
        connecting = null;
        if (made && autoTidy) runTidy(true);
      } else if (!node) {
        connecting = null;
      }
      return;
    }
    if (port && node) {
      connecting = { from: node, port };
      selectedId = null;
      hoverEdge = null;
      fitView();
    } else if (edge) {
      hoverEdge = hoverEdge === edge ? null : edge;
    } else {
      hoverEdge = null;
      selectedId = node ?? null;
      if (node) oninspect?.(node);
    }
  }

  function recordMove(id: string | null) {
    const n = id && !autoTidy ? nodeById(id) : undefined;
    if (n) onmoved?.([{ id: n.id, from: { x: start.nx, y: start.ny }, to: { x: n.x, y: n.y } }]);
  }

  function onTouchUp(e: PointerEvent, cancelled: boolean) {
    if (!fingers.delete(e.pointerId)) return;
    if (host?.hasPointerCapture(e.pointerId)) host.releasePointerCapture(e.pointerId);
    if (mode === 'pinch') {
      if (fingers.size > 1) {
        startPinch();
        return;
      }
      pinch = null;
      pinchK = null;
      const [rest] = fingers.values();
      mode = rest ? 'pan' : 'idle';
      if (rest) start = { px: rest.x, py: rest.y, vx: view.x, vy: view.y, nx: 0, ny: 0, moved: true };
      return;
    }
    if (fingers.size > 0) return;
    const was = mode;
    mode = 'idle';
    if (cancelled) {
      undoSingleFinger();
      return;
    }
    if (!start.moved) {
      wire = null;
      dragging = false;
      activeNode = null;
      tap();
      return;
    }
    if (was === 'node') {
      dragging = false;
      recordMove(activeNode);
      activeNode = null;
      if (autoTidy) runTidy(true);
    }
    if (was === 'wire' && wire) endWire(e);
  }

  function onPointerDown(e: PointerEvent) {
    touch = e.pointerType !== 'mouse';
    if (touch) {
      onTouchDown(e);
      return;
    }
    connecting = null;
    if (e.button !== 0) return;
    const el = e.target as HTMLElement;
    if (el.closest('[data-flow-ui], button')) return;
    (host as HTMLElement).setPointerCapture(e.pointerId);
    start = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y, nx: 0, ny: 0, moved: false };
    closeMenu();

    const port = el.closest<HTMLElement>('[data-port]');
    const nodeEl = el.closest<HTMLElement>('[data-node-id]');
    if (port && nodeEl) {
      const id = nodeEl.dataset.nodeId as string;
      const p = toWorld(e.clientX, e.clientY);
      wire = { from: id, port: port.dataset.port as string, x: p.x, y: p.y };
      mode = 'wire';
      return;
    }
    if (nodeEl) {
      const id = nodeEl.dataset.nodeId as string;
      const n = nodeById(id);
      if (!n) return;
      finishTween();
      activeNode = id;
      selectedId = id;
      oninspect?.(id);
      start.nx = n.x;
      start.ny = n.y;
      mode = 'node';
      return;
    }
    mode = 'pan';
  }

  function onPointerMove(e: PointerEvent) {
    if (mode === 'idle') return;
    const finger = e.pointerType !== 'mouse';
    if (finger) {
      if (!fingers.has(e.pointerId)) return;
      fingers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      if (mode === 'pinch') {
        const [a, b] = fingers.values();
        if (pinch) view = pinchView(pinch, a, b, { min: minZoom(), max: MAX_ZOOM });
        return;
      }
    }
    const dx = e.clientX - start.px;
    const dy = e.clientY - start.py;
    if (!start.moved && Math.hypot(dx, dy) > (finger ? TOUCH_SLOP : DRAG_SLOP)) start.moved = true;
    if (finger && !start.moved) return;
    if (mode === 'pan') {
      view = { ...view, x: start.vx + dx, y: start.vy + dy };
    } else if (mode === 'node' && activeNode && start.moved) {
      dragging = true;
      const n = nodeById(activeNode);
      if (!n) return;
      n.x = snap(start.nx + dx / view.k);
      n.y = snap(start.ny + dy / view.k);
      disp[n.id] = { x: n.x, y: n.y };
    } else if (mode === 'wire' && wire) {
      const p = toWorld(e.clientX, e.clientY);
      wire = { ...wire, x: p.x, y: p.y };
    }
  }

  function onPointerUp(e: PointerEvent) {
    if (e.pointerType !== 'mouse') {
      onTouchUp(e, e.type === 'pointercancel');
      return;
    }
    const was = mode;
    mode = 'idle';
    if (host?.hasPointerCapture(e.pointerId)) host.releasePointerCapture(e.pointerId);
    if (was === 'pan' && !start.moved) selectedId = null;
    if (was === 'node') {
      dragging = false;
      if (start.moved) recordMove(activeNode);
      if (start.moved && autoTidy) runTidy(true);
    }
    if (was === 'wire' && wire) endWire(e);
    activeNode = null;
  }

  function endWire(e: PointerEvent) {
    const w = wire as NonNullable<typeof wire>;
    wire = null;
    if (!start.moved) return;
    const under = document.elementFromPoint(e.clientX, e.clientY)?.closest<HTMLElement>('[data-node-id]');
    if (under) {
      if (connect(flow, { from: w.from, port: w.port, to: under.dataset.nodeId as string }) && autoTidy) runTidy(true);
      return;
    }
    const r = (host as HTMLElement).getBoundingClientRect();
    openMenu({ from: w.from, port: w.port }, { x: w.x, y: w.y }, e.clientX - r.left, e.clientY - r.top);
  }

  async function openMenu(link: { from: string; port: string } | undefined, atPt: Pt, sx: number, sy: number) {
    query = '';
    returnFocus = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null;
    menu = { link, at: atPt, sx, sy };
    await tick();
    menuOpen = true;
  }

  function closeMenu(restore = false) {
    const back = menuByKey && restore ? returnFocus : null;
    menuOpen = false;
    menu = null;
    menuByKey = false;
    returnFocus = null;
    if (back) void tick().then(() => back.isConnected && back.focus({ preventScroll: true }));
  }

  function addFromPort(from: string, port: string, anchor: HTMLElement) {
    const r = (host as HTMLElement).getBoundingClientRect();
    const b = anchor.getBoundingClientRect();
    const n = nodeById(from);
    const w = toWorld(b.right, b.top + b.height / 2);
    openMenu({ from, port }, { x: n ? n.x + NODE_W + GRID : w.x, y: w.y }, b.right - r.left, b.top - r.top + b.height / 2);
  }

  function connectFromMenu() {
    connecting = menu?.link ?? null;
    selectedId = null;
    closeMenu();
    fitView();
  }

  function targetFor(id: string) {
    const n = nodeById(id);
    const c = connecting;
    return Boolean(
      n && c && id !== c.from && specFor(n.type)?.kind !== 'trigger' && !flow.edges.some((e) => e.from === c.from && e.port === c.port && e.to === id)
    );
  }

  function pick(type: string) {
    if (!menu) return;
    const byKey = menuByKey;
    const node = addNode(flow, type, menu.at, menu.link);
    selectedId = node.id;
    oninspect?.(node.id);
    closeMenu();
    if (autoTidy) runTidy(true);
    else disp[node.id] = { x: node.x, y: node.y };
    if (byKey) focusNode(node.id);
  }

  function linkTo(id: string) {
    const link = menu?.link;
    if (!link) return;
    const existing = flow.edges.find((e) => e.from === link.from && e.port === link.port && e.to === id);
    if (existing) removeEdge(flow, existing.id);
    else if (!connect(flow, { from: link.from, port: link.port, to: id })) return;
    announce(existing ? t('flows.noteDisconnected') : t('flows.noteConnected'));
    closeMenu();
    if (autoTidy) runTidy(true);
    focusNode(link.from);
  }

  function onMenuKey(e: KeyboardEvent) {
    stepFocus(e, Array.from((e.currentTarget as HTMLElement).querySelectorAll<HTMLElement>('input, button')));
  }

  function onDragOver(e: DragEvent) {
    if (e.dataTransfer?.types.includes('application/x-flow-node')) {
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    }
  }

  function onDrop(e: DragEvent) {
    const type = e.dataTransfer?.getData('application/x-flow-node');
    if (!type || !specFor(type)) return;
    e.preventDefault();
    const p = toWorld(e.clientX, e.clientY);
    const sel = selectedId ? nodeById(selectedId) : undefined;
    const selSpec = sel && specFor(sel.type);
    const link = sel && selSpec && selSpec.kind !== 'trigger' ? { from: sel.id, port: outputsFor(selSpec)[0].id } : undefined;
    const node = addNode(flow, type, { x: snap(p.x), y: snap(p.y) }, link);
    selectedId = node.id;
    oninspect?.(node.id);
    if (autoTidy) runTidy(true);
    else disp[node.id] = { x: node.x, y: node.y };
  }

  export function announce(message: string) {
    note = '';
    void tick().then(() => (note = message));
  }

  export function focusNode(id: string) {
    void tick().then(() => host?.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(id)}"] [role="button"]`)?.focus({ preventScroll: true }));
  }

  function keepInView(id: string) {
    const n = nodeById(id);
    if (!n || !host) return;
    if (insets?.(true)) {
      revealSelected();
      return;
    }
    const box = { x0: n.x, y0: n.y, x1: n.x + NODE_W + ADD_ROOM, y1: n.y + nodeHeight(specFor(n.type)) };
    const target = revealBox(view, box, { width: host.clientWidth, height: host.clientHeight, inset: { ...KEY_INSET, right: KEY_INSET.right + room } }, view.k);
    if (target !== view) view = target;
  }

  function selectAndShow(id: string) {
    selectedId = id;
    keepInView(id);
    focusNode(id);
  }

  export function walk(dir: Dir): boolean {
    const next = selectedId && nodeById(selectedId) ? neighbor(flow, selectedId, dir) : (readingOrder(flow)[0]?.id ?? null);
    if (next) selectAndShow(next);
    return next !== null;
  }

  export function nudge(dir: Dir): boolean {
    const n = selectedId && !autoTidy ? nodeById(selectedId) : undefined;
    if (!n) return false;
    const { dx, dy } = stepDelta(dir);
    const from = { x: n.x, y: n.y };
    n.x += dx;
    n.y += dy;
    disp[n.id] = { x: n.x, y: n.y };
    onmoved?.([{ id: n.id, from, to: { x: n.x, y: n.y } }]);
    keepInView(n.id);
    return true;
  }

  export function removeSelected(): boolean {
    const id = selectedId;
    if (!id || !nodeById(id)) return false;
    const next = neighborAfterRemove(flow, id);
    removeNode(flow, id);
    selectedId = next;
    if (autoTidy) runTidy(true);
    if (next) selectAndShow(next);
    announce(t('flows.noteRemoved'));
    return true;
  }

  export function duplicateSelected(): boolean {
    const copy = selectedId ? duplicateNode(flow, selectedId) : null;
    if (!copy) return false;
    selectedId = copy.id;
    if (autoTidy) runTidy(true);
    else disp[copy.id] = { x: copy.x, y: copy.y };
    selectAndShow(copy.id);
    announce(t('flows.noteDuplicated'));
    return true;
  }

  export function openAddMenu(other = false): boolean {
    const n = selectedId ? nodeById(selectedId) : undefined;
    const spec = n && specFor(n.type);
    if (!n || !spec) return false;
    const outs = outputsFor(spec);
    const port = outs[other ? outs.length - 1 : 0].id;
    const anchor = host?.querySelector(`[data-node-id="${CSS.escape(n.id)}"] [data-port="${CSS.escape(port)}"]`)?.parentElement?.querySelector('button');
    if (!anchor) return false;
    menuByKey = true;
    addFromPort(n.id, port, anchor);
    return true;
  }

  export function dismissMenu(): boolean {
    if (!menu) return false;
    closeMenu(true);
    return true;
  }

  export function zoomBy(factor: number) {
    zoomCenter(factor);
  }

  export function settle(retidy: boolean) {
    if (retidy && autoTidy) runTidy(true);
    else finishTween();
  }

  function deleteEdge(id: string) {
    removeEdge(flow, id);
    hoverEdge = null;
    if (autoTidy) runTidy(true);
  }

  function holdEdge(e: PointerEvent, id: string) {
    if (e.pointerType !== 'mouse') return;
    clearTimeout(hoverTimer);
    hoverEdge = id;
  }

  function leaveEdge(e: PointerEvent) {
    if (e.pointerType !== 'mouse') return;
    clearTimeout(hoverTimer);
    hoverTimer = setTimeout(() => (hoverEdge = null), 140);
  }

  const dotGap = $derived(GRID * view.k * (view.k < 0.6 ? 2 : 1));

  const edges = $derived(
    flow.edges.flatMap((e) => {
      const from = nodeById(e.from);
      const to = nodeById(e.to);
      if (!from || !to) return [];
      const spec = specFor(from.type);
      const a = at(from.id);
      const b = at(to.id);
      const p1 = { x: a.x + NODE_W, y: a.y + portY(spec, e.port) };
      const p2 = { x: b.x - ARROW_GAP, y: b.y + inputY };
      return [{ id: e.id, d: wirePath(p1, p2), mid: { x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2 }, tone: e.port === 'no' ? 'no' : 'yes' }];
    })
  );

  const stopId = $derived(selectedId && nodeById(selectedId) ? selectedId : (readingOrder(flow)[0]?.id ?? null));

  const triggers = $derived(flow.nodes.filter((n) => specFor(n.type)?.kind === 'trigger'));

  const startWires = $derived(
    triggers.map((n) => {
      const b = at(n.id);
      const from = { x: startAt.x + START_W, y: startAt.y + START_H / 2 };
      return { id: n.id, d: wirePath(from, { x: b.x - ARROW_GAP, y: b.y + inputY }) };
    })
  );

  const wireD = $derived.by(() => {
    if (!wire) return '';
    const n = nodeById(wire.from);
    if (!n) return '';
    const a = at(n.id);
    return wirePath({ x: a.x + NODE_W, y: a.y + portY(specFor(n.type), wire.port) }, { x: wire.x, y: wire.y });
  });

  const needle = $derived(query.trim().toLowerCase());
  const groups = $derived(
    MENU_KINDS.map((g) => ({
      ...g,
      specs: NODE_SPECS.filter(
        (s) => s.kind !== 'trigger' && s.kind === g.kind && (!needle || `${s.label} ${s.summary}`.toLowerCase().includes(needle))
      )
    })).filter((g) => g.specs.length > 0)
  );

  const links = $derived.by(() => {
    const link = menu?.link;
    if (!link || (touch && !menuByKey)) return [];
    return flow.nodes.flatMap((n) => {
      const spec = specFor(n.type);
      if (!spec || spec.kind === 'trigger' || n.id === link.from) return [];
      const line = describe(n);
      if (needle && !`${spec.label} ${line}`.toLowerCase().includes(needle)) return [];
      return [{ id: n.id, spec, line, linked: flow.edges.some((e) => e.from === link.from && e.port === link.port && e.to === n.id) }];
    });
  });
</script>

<div
  class="canvas"
  class:dragging
  class:view-open={viewOpen}
  data-touch={touch ? '' : undefined}
  data-connecting={connecting ? '' : undefined}
  style:background-size="{dotGap}px {dotGap}px"
  style:background-position="{view.x}px {view.y}px"
  bind:this={host}
  role="application"
  aria-label="Flow board"
  data-flow-canvas
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  onpointercancel={onPointerUp}
  ondragover={onDragOver}
  ondrop={onDrop}
>
  <div class="world" style:transform="translate({view.x}px, {view.y}px) scale({view.k})" style:--inv={touch ? 1 / shownK : undefined}>
    <svg class="wires" width="1" height="1" aria-hidden="true">
      <defs>
        <marker id="flow-arrow-yes" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto">
          <path d="M0,0 L10,5 L0,10 z" style="fill: rgba(var(--bb-green-glow-rgb), 0.7)" />
        </marker>
        <marker id="flow-arrow-no" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto">
          <path d="M0,0 L10,5 L0,10 z" style="fill: rgba(var(--bb-muted-rgb), 0.6)" />
        </marker>
      </defs>
      {#each startWires as wire (wire.id)}
        <path
          class="wire"
          class:taken={trace?.nodes.has(wire.id)}
          class:off={trace && !trace.nodes.has(wire.id)}
          d={wire.d}
          marker-end="url(#flow-arrow-yes)"
        />
      {/each}
      {#each edges as edge (edge.id)}
        <path
          class="wire"
          class:hot={hoverEdge === edge.id}
          class:no={edge.tone === 'no'}
          class:taken={trace?.edges.has(edge.id)}
          class:off={trace && !trace.edges.has(edge.id)}
          d={edge.d}
          marker-end="url(#flow-arrow-{edge.tone})"
        />
        <path
          class="hit"
          d={edge.d}
          data-edge={edge.id}
          role="presentation"
          onpointerenter={(e) => holdEdge(e, edge.id)}
          onpointerleave={leaveEdge}
        />
      {/each}
      {#if wire}
        <path class="wire preview" d={wireD} />
      {/if}
    </svg>

    {#each edges as edge (edge.id)}
      {#if hoverEdge === edge.id}
        <span
          class="cut"
          data-flow-ui
          style:transform="translate({edge.mid.x - 14}px, {edge.mid.y - 14}px)"
          role="presentation"
          onpointerenter={(e) => holdEdge(e, edge.id)}
          onpointerleave={leaveEdge}
        >
          <IconButton label="Remove connection" size="sm" tone="danger" onclick={() => deleteEdge(edge.id)}>
            <Icon name="x" size={12} />
          </IconButton>
        </span>
      {/if}
    {/each}

    <FlowStart x={startAt.x} y={startAt.y} empty={triggers.length === 0} onadd={onstart} />

    {#each flow.nodes as node (node.id)}
      <div
        class="slot"
        class:off={trace && !trace.nodes.has(node.id)}
        class:running={trace?.current === node.id}
        class:source={connecting?.from === node.id}
        class:target={connecting && targetFor(node.id)}
        data-node-id={node.id}
        style:transform="translate({at(node.id).x}px, {at(node.id).y}px)"
        style:z-index={selectedId === node.id || trace?.current === node.id ? 2 : 1}
        role="presentation"
        onfocusin={(e) => {
          if (!(e.target as HTMLElement).closest('button')) selectedId = node.id;
          if (host) {
            host.scrollLeft = 0;
            host.scrollTop = 0;
          }
          if (mode === 'idle') keepInView(node.id);
        }}
      >
        <FlowNodeCard {node} selected={selectedId === node.id} tabbable={stopId === node.id} onadd={(port, el) => addFromPort(node.id, port, el)} />
      </div>
    {/each}
  </div>

  {#if menu}
    <span class="menu-anchor" bind:this={menuAnchor} style:left="{menu.sx}px" style:top="{menu.sy}px"></span>
    <PickerPanel bind:open={menuOpen} anchor={menuAnchor} label="Add a step" width={300} maxHeight={380} onClose={() => closeMenu(true)}>
      <div class="menu" role="presentation" onkeydown={onMenuKey}>
        {#if touch && menu.link}
          <PickerOption onclick={connectFromMenu}>
            <span class="menu-row">
              <IconTile class="menu-tile"><Icon name="branch" size={16} /></IconTile>
              <span class="menu-name">{t('flows.connectExisting')}</span>
            </span>
          </PickerOption>
        {/if}
        <SearchInput value={query} onValueChange={(v) => (query = v)} placeholder="Search steps" fill />
        {#each groups as g (g.kind)}
          <span class="menu-title">{g.title}</span>
          {#each g.specs as s (s.type)}
            <PickerOption onclick={() => pick(s.type)}>
              <span class="menu-row" style:--tint={MENU_TINT[s.kind].color} style:--tint-rgb={MENU_TINT[s.kind].rgb}>
                <IconTile class="menu-tile"><Icon name={s.icon} size={16} /></IconTile>
                <span class="menu-text">
                  <span class="menu-name">{s.label}</span>
                  <span class="menu-sum">{s.summary}</span>
                </span>
              </span>
            </PickerOption>
          {/each}
        {:else}
          {#if links.length === 0}<span class="menu-sum menu-empty">No steps match.</span>{/if}
        {/each}
        {#if links.length}
          <span class="menu-title">{t('flows.linkTitle')}</span>
          {#each links as l (l.id)}
            <PickerOption onclick={() => linkTo(l.id)}>
              <span class="menu-row" style:--tint={MENU_TINT[l.spec.kind].color} style:--tint-rgb={MENU_TINT[l.spec.kind].rgb}>
                <IconTile class="menu-tile"><Icon name={l.spec.icon} size={16} /></IconTile>
                <span class="menu-text">
                  <span class="menu-name">{l.spec.label}</span>
                  <span class="menu-sum">{l.linked ? t('flows.linkedRemove') : l.line}</span>
                </span>
              </span>
            </PickerOption>
          {/each}
        {/if}
      </div>
    </PickerPanel>
  {/if}

  {#if connecting}
    <div class="connect-hint" data-flow-ui role="status">
      <span>{t('flows.connectHint')}</span>
      <Button variant="ghost" onclick={() => (connecting = null)}>{t('flows.connectCancel')}</Button>
    </div>
  {/if}

  <button class="view-toggle" type="button" aria-expanded={viewOpen} onclick={() => (viewOpen = !viewOpen)}>
    <Icon name="settings" size={22} />
    <span>{t('flows.viewTools')}</span>
  </button>

  <div class="zoom" data-flow-ui>
    <Cluster gap={2} align="stretch" nowrap>
      <IconButton label="Zoom out" size="sm" tooltip onclick={() => zoomCenter(1 / 1.2)}><Icon name="minus" size={14} /></IconButton>
      <span class="zoom-pct">{Math.round(shownK * 100)}%</span>
      <IconButton label="Zoom in" size="sm" tooltip onclick={() => zoomCenter(1.2)}><Icon name="plus" size={14} /></IconButton>
      <IconButton label="Fit to view" size="sm" tooltip onclick={() => fitView()}><Icon name="expand" size={14} /></IconButton>
    </Cluster>
    <span class="zoom-gap" aria-hidden="true"></span>
    <IconButton label="Clean up" size="sm" tooltip onclick={tidyNow}><Icon name="tidy" size={14} /></IconButton>
    <label class="auto">
      <span>Auto tidy</span>
      <Switch bind:checked={autoTidy} label="Auto tidy" />
    </label>
  </div>

  <VisuallyHidden as="p" role="status" aria-live="polite">{note}</VisuallyHidden>
</div>

<style>
  .canvas {
    position: relative;
    width: 100%;
    height: 100%;
    overflow: hidden;
    background-color: var(--bb-black);
    background-image: radial-gradient(circle, rgba(var(--bb-white-rgb), 0.16) 1px, transparent 1.5px);
    touch-action: none;
    cursor: default;
    user-select: none;
    -webkit-touch-callout: none;
    -webkit-tap-highlight-color: transparent;
  }
  .canvas.dragging {
    cursor: grabbing;
  }
  .world {
    position: absolute;
    left: 0;
    top: 0;
    width: 0;
    height: 0;
    transform-origin: 0 0;
  }
  .wires {
    position: absolute;
    left: 0;
    top: 0;
    overflow: visible;
    pointer-events: none;
  }
  .wire {
    fill: none;
    stroke: rgba(var(--bb-green-glow-rgb), 0.7);
    stroke-width: 2;
    stroke-linecap: round;
    transition: stroke var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .wire.no {
    stroke: rgba(var(--bb-muted-rgb), 0.6);
  }
  .wire.hot {
    stroke: var(--bb-green-glow);
  }
  .wire.no.hot {
    stroke: var(--bb-muted-light);
  }
  .wire.taken {
    stroke: var(--bb-green-glow);
    stroke-width: 3;
  }
  .wire.no.taken {
    stroke: var(--bb-white);
  }
  .wire.off {
    opacity: 0.2;
  }
  .wire.preview {
    stroke: rgba(var(--bb-white-rgb), 0.5);
    stroke-dasharray: 6 6;
  }
  .hit {
    fill: none;
    stroke: transparent;
    stroke-width: 16;
    pointer-events: stroke;
  }
  .slot {
    position: absolute;
    left: 0;
    top: 0;
    will-change: transform;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
    border-radius: var(--bb-radius-md);
  }
  .slot.off {
    opacity: 0.35;
  }
  .slot.running :global(.node) {
    box-shadow:
      0 0 0 2px var(--bb-green-glow),
      0 0 28px rgba(var(--bb-green-glow-rgb), 0.35);
  }
  .cut {
    position: absolute;
    left: 0;
    top: 0;
    z-index: 4;
  }
  .menu-anchor {
    position: absolute;
    width: 1px;
    height: 1px;
    pointer-events: none;
  }
  .menu {
    display: grid;
    gap: var(--bb-space-2);
    padding: var(--bb-space-2);
    font-family: var(--bb-font-body);
  }
  .menu-title {
    padding: var(--bb-space-2) var(--bb-space-2) 0;
    font-size: 14px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .menu-row {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .menu-row :global(.menu-tile) {
    --tile: 32px;
    --tile-bg: rgba(var(--tint-rgb), 0.14);
    color: var(--tint);
  }
  .menu-text {
    min-width: 0;
    display: grid;
    gap: 1px;
  }
  .menu-name {
    font-size: 15px;
    font-weight: 600;
    line-height: 20px;
    color: var(--bb-white);
  }
  .menu-sum {
    font-size: 14px;
    line-height: 18px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .menu-empty {
    padding: var(--bb-space-2);
  }
  .zoom {
    position: absolute;
    left: var(--bb-space-4);
    bottom: var(--bb-space-4);
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 6px 12px 6px 6px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.5);
    z-index: 5;
  }
  .zoom-gap {
    width: 12px;
  }
  .auto {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    margin-inline-start: 8px;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
    white-space: nowrap;
  }
  .zoom-pct {
    min-width: 44px;
    align-self: center;
    text-align: center;
    font-family: var(--bb-font-body);
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .view-toggle {
    display: none;
  }
  .connect-hint {
    position: absolute;
    top: calc(var(--editor-top) + 80px);
    left: 50%;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 12px;
    width: max-content;
    max-width: calc(100% - 32px);
    padding: 6px 6px 6px 16px;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: 14px;
    translate: -50% 0;
  }
  .canvas[data-connecting] .slot {
    opacity: 0.35;
  }
  .canvas[data-connecting] .slot.source,
  .canvas[data-connecting] .slot.target {
    opacity: 1;
  }
  .slot.source :global(.node) {
    box-shadow:
      0 0 0 2px var(--bb-tan-light),
      0 0 28px rgba(var(--bb-tan-light-rgb), 0.18);
  }
  .slot.target :global(.node) {
    box-shadow:
      0 0 0 2px var(--bb-green-glow),
      0 0 28px rgba(var(--bb-green-glow-rgb), 0.25);
  }
  .canvas[data-touch] .hit {
    stroke-width: calc(32px * var(--inv, 1));
  }
  .canvas[data-touch] .slot::before {
    content: '';
    position: absolute;
    top: 50%;
    left: 0;
    right: 0;
    height: max(100%, 44px * var(--inv, 1));
    translate: 0 -50%;
  }
  .canvas[data-touch] .cut :global(button) {
    border-radius: var(--bb-radius-pill);
    background: var(--bb-bg-1);
    box-shadow: 0 2px 8px rgba(var(--bb-shadow-rgb), 0.6);
    scale: calc(var(--inv, 1) * 1.6);
  }
  .canvas[data-touch] .zoom :global(.bb-btn--icon),
  .canvas[data-touch] .connect-hint :global(button) {
    min-width: 44px;
    width: 44px;
    height: 44px;
  }
  .canvas[data-touch] .connect-hint :global(button) {
    width: auto;
  }
  .canvas[data-touch] .auto {
    min-height: 44px;
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    .view-toggle {
      position: absolute;
      right: max(16px, env(safe-area-inset-right, 0px));
      bottom: calc(var(--flow-dock-room) + 8px);
      z-index: 5;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      gap: 4px;
      width: 64px;
      height: 68px;
      padding: 0;
      border: 0;
      border-radius: 12px;
      background: var(--bb-bg-1);
      box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
      color: rgba(var(--bb-white-rgb), 0.92);
      font-family: var(--bb-font-body);
      font-size: 14px;
    }
    .view-toggle[aria-expanded='true'] {
      background: var(--bb-elev-2);
      color: var(--bb-white);
    }
    .zoom {
      left: max(16px, env(safe-area-inset-left, 0px));
      right: max(16px, env(safe-area-inset-right, 0px));
      bottom: calc(var(--flow-dock-room) + 84px);
      display: none;
      flex-wrap: wrap;
      justify-content: space-between;
    }
    .canvas.view-open .zoom {
      display: flex;
    }
    .zoom-gap {
      display: none;
    }
    .auto {
      margin-inline-start: 4px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .wire {
      transition: none;
    }
  }
</style>
