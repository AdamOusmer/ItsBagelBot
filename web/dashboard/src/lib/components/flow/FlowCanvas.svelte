<script lang="ts" module>
  import type { Dir } from '../../flow/keyboard';

  export type FlowTrace = { nodes: ReadonlySet<string>; edges: ReadonlySet<string>; current: string | null };

  export type FlowCanvasApi = {
    tidyNow: () => void;
    fitView: (reservedRight?: number) => void;
    announce: (message: string) => void;
    focusNode: (id: string) => void;
    revealNode: (id: string) => void;
    walk: (dir: Dir) => boolean;
    nudge: (dir: Dir) => boolean;
    removeSelected: () => boolean;
    removeStep: (id: string) => boolean;
    duplicateSelected: () => boolean;
    openAddMenu: (other?: boolean) => boolean;
    dismissMenu: () => boolean;
    zoomBy: (factor: number) => void;
    settle: (retidy: boolean) => void;
    nodeIdAt: (el: unknown) => string | undefined;
    hasTarget: (el: unknown) => boolean;
  };
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/dot-canvas.css';
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
  import { GRID, NODE_W, outputsFor, specFor, type Flow, type FlowKind } from '../../flow/model';
  import { connect, duplicateNode, removeEdge } from '../../flow/ops';
  import { addStep, connectionTarget, parallelPorts, portOccupied, removeStep as deleteStep, selectedPlacement } from '../../flow/editor';
  import { currentPositions, tweenPositions } from '../../flow/canvas-positions';
  import { edgePaths, flowBounds, loopPaths, startPaths, wirePreview } from '../../flow/canvas-geometry';
  import { searchSteps } from '../../flow/search';
  import { neighbor, readingOrder, stepDelta, stepIndex } from '../../flow/keyboard';
  import type { Move } from '../../flow/history';
  import { stepLabel, stepSummary } from '../../flow/copy';
  import { nodeHeight, tidy } from '../../flow/layout';
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
    oninspect,
    onadded,
    onremoved,
    onundo,
    onredo,
    canUndo = false,
    canRedo = false
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
    onadded?: (id: string) => void;
    onremoved?: (count: number) => void;
    onundo?: () => void;
    onredo?: () => void;
    canUndo?: boolean;
    canRedo?: boolean;
  } = $props();

  const { t } = getI18n();

  const startAt: Pt = { x: -START_W - 56, y: -24 };

  type Pt = { x: number; y: number };
  type NodeId = Flow['nodes'][number]['id'];
  type PortId = string;
  type NodeType = Flow['nodes'][number]['type'];
  type Link = { from: NodeId; port: PortId };
  const MIN_ZOOM = 0.4;
  const MAX_ZOOM = 1.6;
  const TIDY_MS = 320;
  const FIT_INSET = { top: 160, right: 48, bottom: 96, left: 120 };
  const KEY_INSET = { top: 104, right: 24, bottom: 80, left: 112 };
  const DRAG_SLOP = 3;
  const TOUCH_SLOP = 8;
  const TOUCH_MIN_ZOOM = 0.25;
  const READABLE_ZOOM = 0.75;
  const ADD_ROOM = 48;
  const GLIDE_MS = 240;
  const MENU_TINT: Record<FlowKind, { color: string; rgb: string }> = {
    trigger: { color: 'var(--bb-green-glow)', rgb: 'var(--bb-green-glow-rgb)' },
    condition: { color: 'var(--bb-tan)', rgb: 'var(--bb-tan-rgb)' },
    reply: { color: 'var(--bb-muted-light)', rgb: 'var(--bb-muted-rgb)' }
  };

  let host = $state<HTMLDivElement>();
  let zoomEl = $state<HTMLElement | null>(null);
  let hintEl = $state<HTMLElement | null>(null);
  let edgeToolbarEl = $state<HTMLElement | null>(null);
  let startEl = $state<HTMLElement | null>(null);
  const nodeEls: Record<string, HTMLElement | null> = {};
  const cards: Record<string, { portAt: (el: Element | null) => string | undefined; addButton: (port: string) => HTMLButtonElement | null } | null> = {};
  const edgeEls: Record<string, SVGElement | null> = {};
  let view = $state({ x: 0, y: 0, k: 1 });
  let disp = $state<Record<string, Pt>>({});
  let dragging = $state(false);
  let wire = $state<{ from: string; port: string; x: number; y: number } | null>(null);
  let hoverEdge = $state<string | null>(null);
  let selectedEdge = $state<string | null>(null);
  let wireTarget = $state<string | null>(null);
  let toolbarBottom = $state(20);
  let menu = $state<{ link?: { from: string; port: string }; at: Pt; sx: number; sy: number } | null>(null);
  let menuAnchor = $state<HTMLElement>();
  let menuOpen = $state(false);
  let query = $state('');
  let menuSearch = $state<HTMLInputElement>();
  let activeResult = $state(0);
  let insertMode = $state<'insert' | 'parallel'>('insert');
  const pickerId = $props.id();
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
  let glide = 0;
  let toolbarFrame = 0;
  const fingers = new Map<number, Pt>();
  let pinch: Pinch | null = null;
  let tapped: { port?: string; node?: string; edge?: string } = {};

  const minZoom = () => (touch ? TOUCH_MIN_ZOOM : MIN_ZOOM);
  const shownK = $derived(pinchK ?? view.k);

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;
  const nodeById = (id: NodeId) => flow.nodes.find((n) => n.id === id);
  const at = (id: NodeId): Pt => disp[id] ?? nodeById(id) ?? { x: 0, y: 0 };
  const snap = (v: number) => Math.round(v / GRID) * GRID;

  export const nodeIdAt = (el: unknown): string | undefined => Object.keys(nodeEls).find((id) => nodeEls[id]?.contains(el as Node | null));
  export const hasTarget = (el: unknown): boolean => Boolean(host?.contains(el as Node | null));
  const portAt = (id: NodeId | undefined, el: Element | null) => (id ? cards[id]?.portAt(el) : undefined);
  const edgeAt = (el: Element | null) => Object.keys(edgeEls).find((id) => edgeEls[id] === el);

  function inOwnUi(el: Element, withStart: boolean): boolean {
    const surfaces = withStart ? [zoomEl, hintEl, edgeToolbarEl, startEl] : [zoomEl, hintEl, edgeToolbarEl];
    return el.closest('button') !== null || surfaces.some((s) => s?.contains(el));
  }

  function toWorld(clientX: number, clientY: number): Pt {
    const r = (host as HTMLElement).getBoundingClientRect();
    return { x: (clientX - r.left - view.x) / view.k, y: (clientY - r.top - view.y) / view.k };
  }

  function finishTween() {
    cancelAnimationFrame(tween);
    clearTimeout(tweenTimer);
    disp = currentPositions(flow.nodes);
  }

  function applyPositions(targets: Map<string, Pt>, animate: boolean) {
    finishTween();
    const from = currentPositions(flow.nodes, disp);
    flow.nodes.forEach((node) => Object.assign(node, targets.get(node.id) ?? {}));
    if (!animate || reduced()) {
      disp = currentPositions(flow.nodes);
      return;
    }
    const t0 = performance.now();
    const step = (now: number) => {
      const p = Math.min(1, (now - t0) / TIDY_MS);
      disp = tweenPositions(flow.nodes, from, p);
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
    if (!host) return;
    if (!n || !room) return;
    const box = { x0: n.x, y0: n.y, x1: n.x + NODE_W + ADD_ROOM, y1: n.y + nodeHeight(specFor(n.type)) };
    const target = revealBox(view, box, { width: host.clientWidth, height: host.clientHeight, inset: room }, READABLE_ZOOM);
    if (target !== view) glideTo(target);
  }

  export function tidyNow() {
    runTidy(true);
  }

  export function fitView(reservedRight = 0) {
    if (!host) return;
    const bounds = flowBounds(flow, { x0: startAt.x, y0: startAt.y, x1: startAt.x + START_W, y1: startAt.y + START_H });
    const inset = insets?.(reservedRight > 0) ?? { ...FIT_INSET, right: FIT_INSET.right + reservedRight };
    cancelAnimationFrame(glide);
    view = fitBox(bounds, { width: host.clientWidth, height: host.clientHeight, inset }, { min: minZoom(), max: MAX_ZOOM });
  }

  function zoomAt(factor: number, { x: px, y: py }: Pt) {
    const k = Math.min(MAX_ZOOM, Math.max(minZoom(), view.k * factor));
    const wx = (px - view.x) / view.k;
    const wy = (py - view.y) / view.k;
    view = { k, x: px - wx * k, y: py - wy * k };
  }

  function zoomCenter(factor: number) {
    if (host) zoomAt(factor, { x: host.clientWidth / 2, y: host.clientHeight / 2 });
  }

  function onWheel(e: WheelEvent) {
    e.preventDefault();
    const r = (host as HTMLElement).getBoundingClientRect();
    if (e.ctrlKey || e.metaKey) zoomAt(Math.exp(-e.deltaY * 0.01), { x: e.clientX - r.left, y: e.clientY - r.top });
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
  const stopCompatClick = (e: TouchEvent) => {
    if (!inOwnUi(e.target as Element, false)) e.preventDefault();
  };

  function scheduleToolbarPosition() {
    cancelAnimationFrame(toolbarFrame);
    toolbarFrame = requestAnimationFrame(() => {
      toolbarBottom = Math.max(20, (insets?.(room > 0)?.bottom ?? 0) + 8);
    });
  }

  onMount(() => {
    const observer = new ResizeObserver(scheduleToolbarPosition);
    if (host) observer.observe(host);
    touch = matchMedia('(pointer: coarse)').matches;
    runTidy(false);
    fitView();
    host?.addEventListener('keydown', onCanvasKey);
    host?.addEventListener('wheel', onWheel, { passive: false });
    host?.addEventListener('gesturestart', stopGesture);
    host?.addEventListener('touchend', stopCompatClick, { passive: false });
    return () => {
      observer.disconnect();
      cancelAnimationFrame(toolbarFrame);
      host?.removeEventListener('keydown', onCanvasKey);
      host?.removeEventListener('wheel', onWheel);
      host?.removeEventListener('gesturestart', stopGesture);
      host?.removeEventListener('touchend', stopCompatClick);
      cancelAnimationFrame(tween);
      cancelAnimationFrame(glide);
      clearTimeout(tweenTimer);
    };
  });

  $effect(() => {
    void selectedEdge;
    void room;
    scheduleToolbarPosition();
  });

  $effect(() => {
    for (const n of flow.nodes) if (!disp[n.id]) disp[n.id] = { x: n.x, y: n.y };
  });

  function onTouchDown(e: PointerEvent) {
    const aimed = e.target as HTMLElement;
    viewOpen = viewOpen && Boolean(aimed.closest('.zoom, .view-toggle'));
    if (inOwnUi(aimed, false)) return;
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
    const id = nodeIdAt(el);
    const port = portAt(id, el);
    tapped = { port, node: id, edge: edgeAt(el) };
    mode = 'pan';
    if (connecting) return;
    if (port && id) return startWire({ from: id, port }, e);
    if (id) startNode(id, false);
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
    wireTarget = null;
  }

  function startPinch() {
    undoSingleFinger();
    const [a, b] = fingers.values();
    pinch = beginPinch(view, a, b);
    pinchK = view.k;
    mode = 'pinch';
  }

  function tap() {
    blurActive();
    const { port, node, edge } = tapped;
    if (connecting) return completeConnection(node);
    if (port && node) {
      connecting = { from: node, port };
      selectedId = null;
      hoverEdge = null;
      fitView();
      return;
    }
    if (edge) return selectEdge(edge);
    hoverEdge = null;
    selectedEdge = null;
    selectedId = node ?? null;
    if (node) oninspect?.(node);
  }

  function blurActive() {
    const focused = document.activeElement;
    if (focused instanceof HTMLElement) focused.blur();
  }

  function recordMove(id: NodeId | null) {
    const n = id && !autoTidy ? nodeById(id) : undefined;
    if (n) onmoved?.([{ id: n.id, from: { x: start.nx, y: start.ny }, to: { x: n.x, y: n.y } }]);
  }

  function onTouchUp(e: PointerEvent, cancelled: boolean) {
    if (!fingers.delete(e.pointerId)) return;
    releasePointer(e);
    if (mode === 'pinch') return endPinch();
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
    finishGesture(was, e);
  }

  function releasePointer(e: PointerEvent) {
    if (host?.hasPointerCapture(e.pointerId)) host.releasePointerCapture(e.pointerId);
  }

  function finishGesture(was: typeof mode, e: PointerEvent) {
    if (was === 'node') completeNodeDrag();
    if (was === 'wire' && wire) endWire(e);
  }

  function endPinch() {
    if (fingers.size > 1) return startPinch();
    pinch = null;
    pinchK = null;
    const [rest] = fingers.values();
    mode = rest ? 'pan' : 'idle';
    if (rest) start = { px: rest.x, py: rest.y, vx: view.x, vy: view.y, nx: 0, ny: 0, moved: true };
  }

  function connectTo(id: NodeId, source: Link): boolean {
    if (!targetFor(id, source)) return false;
    if (!connect(flow, { from: source.from, port: source.port, to: id })) return false;
    announce(t('flows.noteConnected'));
    if (autoTidy) runTidy(true);
    return true;
  }

  function completeConnection(id: NodeId | undefined) {
    if (!connecting) return;
    if (!id || connectTo(id, connecting)) connecting = null;
  }

  function startWire(source: Link, e: PointerEvent) {
    const p = toWorld(e.clientX, e.clientY);
    wire = { ...source, x: p.x, y: p.y };
    selectedEdge = null;
    wireTarget = null;
    mode = 'wire';
  }

  function startNode(id: NodeId, inspect: boolean) {
    const n = nodeById(id);
    if (!n) return;
    finishTween();
    activeNode = id;
    start.nx = n.x;
    start.ny = n.y;
    mode = 'node';
    if (!inspect) return;
    selectedEdge = null;
    selectedId = id;
    oninspect?.(id);
  }

  function completeNodeDrag() {
    dragging = false;
    if (!start.moved) return;
    recordMove(activeNode);
    activeNode = null;
    if (autoTidy) runTidy(true);
  }

  function onPointerDown(e: PointerEvent) {
    touch = e.pointerType !== 'mouse';
    if (touch) return onTouchDown(e);
    if (e.button !== 0) return;
    const el = e.target as HTMLElement;
    if (inOwnUi(el, true)) return;
    if (connecting) return completeConnection(nodeIdAt(el));
    (host as HTMLElement).setPointerCapture(e.pointerId);
    start = { px: e.clientX, py: e.clientY, vx: view.x, vy: view.y, nx: 0, ny: 0, moved: false };
    closeMenu();
    pointerTarget(e, el);
  }

  function pointerTarget(e: PointerEvent, el: HTMLElement) {
    const edge = edgeAt(el);
    if (edge) {
      selectEdge(edge);
      mode = 'idle';
      return;
    }
    const id = nodeIdAt(el);
    const port = portAt(id, el);
    if (port && id) return startWire({ from: id, port }, e);
    if (id) return startNode(id, true);
    mode = 'pan';
  }

  function onPointerMove(e: PointerEvent) {
    if (mode === 'idle') return;
    if (trackFinger(e)) return;
    const finger = e.pointerType !== 'mouse';
    const dx = e.clientX - start.px;
    const dy = e.clientY - start.py;
    markMoved({ x: dx, y: dy }, finger);
    if (finger && !start.moved) return;
    moveGesture(e, { x: dx, y: dy });
  }

  function markMoved(delta: Pt, finger: boolean) {
    const threshold = finger ? TOUCH_SLOP : DRAG_SLOP;
    if (!start.moved && Math.hypot(delta.x, delta.y) > threshold) start.moved = true;
  }

  function moveGesture(e: PointerEvent, { x: dx, y: dy }: Pt) {
    if (mode === 'pan') {
      view = { ...view, x: start.vx + dx, y: start.vy + dy };
      return;
    }
    if (mode === 'node' && start.moved) return dragNode(dx, dy);
    if (mode === 'wire') moveWire(e);
  }

  function trackFinger(e: PointerEvent): boolean {
    if (e.pointerType === 'mouse') return false;
    if (!fingers.has(e.pointerId)) return true;
    fingers.set(e.pointerId, { x: e.clientX, y: e.clientY });
    if (mode !== 'pinch') return false;
    const [a, b] = fingers.values();
    if (pinch) view = pinchView(pinch, a, b, { min: minZoom(), max: MAX_ZOOM });
    return true;
  }

  function dragNode(dx: number, dy: number) {
    const n = activeNode ? nodeById(activeNode) : undefined;
    if (!n) return;
    dragging = true;
    n.x = snap(start.nx + dx / view.k);
    n.y = snap(start.ny + dy / view.k);
    disp[n.id] = { x: n.x, y: n.y };
  }

  function moveWire(e: PointerEvent) {
    if (!wire) return;
    const p = toWorld(e.clientX, e.clientY);
    wireTarget = nodeIdAt(document.elementFromPoint(e.clientX, e.clientY)) ?? null;
    wire = { ...wire, x: p.x, y: p.y };
  }

  function onPointerUp(e: PointerEvent) {
    if (e.pointerType !== 'mouse') {
      onTouchUp(e, e.type === 'pointercancel');
      return;
    }
    const was = mode;
    mode = 'idle';
    releasePointer(e);
    if (e.type === 'pointercancel') {
      undoSingleFinger();
      return;
    }
    if (was === 'pan' && !start.moved) { selectedId = null; selectedEdge = null; }
    finishGesture(was, e);
    activeNode = null;
  }

  function endWire(e: PointerEvent) {
    const w = wire as NonNullable<typeof wire>;
    wire = null;
    wireTarget = null;
    if (!start.moved) return;
    const under = nodeIdAt(document.elementFromPoint(e.clientX, e.clientY));
    if (under) {
      connectTo(under, w);
      return;
    }
    const r = (host as HTMLElement).getBoundingClientRect();
    openMenu({ from: w.from, port: w.port }, { x: w.x, y: w.y }, e.clientX - r.left, e.clientY - r.top);
  }

  async function openMenu(link: Link | undefined, atPt: Pt, sx: number, sy: number) {
    query = '';
    activeResult = 0;
    insertMode = 'insert';
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

  function addFromPort(from: NodeId, port: PortId, anchor: HTMLElement) {
    const r = (host as HTMLElement).getBoundingClientRect();
    const b = anchor.getBoundingClientRect();
    const n = nodeById(from);
    const w = toWorld(b.right, b.top + b.height / 2);
    openMenu({ from, port }, { x: n ? n.x + NODE_W + GRID : w.x, y: w.y }, b.right - r.left, b.top - r.top + b.height / 2);
  }

  function connectFromMenu() {
    const byKey = menuByKey;
    connecting = menu?.link ?? null;
    selectedId = null;
    closeMenu();
    fitView();
    if (byKey) {
      const first = readingOrder(flow).find((node) => targetFor(node.id));
      if (first) focusNode(first.id);
    }
  }

  function targetFor(id: NodeId, c = connecting ?? wire ?? menu?.link) {
    return connectionTarget(flow, id, c);
  }

  function pick(type: NodeType) {
    if (!menu) return;
    const placement = menu.link ? { kind: 'port' as const, ...menu.link, mode: insertMode } : { kind: 'canvas' as const };
    const { node } = addStep(flow, { type, at: menu.at, placement, chatMessage: t('flows.board.defaultChatMessage') });
    closeMenu();
    showAdded(node);
  }

  function showAdded(node: Flow['nodes'][number]) {
    selectedId = node.id;
    if (autoTidy) runTidy(true);
    else disp[node.id] = { x: node.x, y: node.y };
    if (onadded) onadded(node.id);
    else focusNode(node.id);
  }

  function onCanvasKey(e: KeyboardEvent) {
    if (!connecting) return;
    if (e.key !== 'Enter' && e.key !== ' ') return;
    const id = nodeIdAt(e.target);
    if (!id || !targetFor(id)) return;
    if (connectTo(id, connecting)) connecting = null;
    e.preventDefault();
    e.stopPropagation();
  }

  function onMenuKey(e: KeyboardEvent) {
    if (e.target !== menuSearch || e.isComposing) return;
    if (e.key === 'Enter') return pickActive(e);
    if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
    e.preventDefault();
    activeResult = stepIndex(activeResult, results.length, e.key);
  }

  function pickActive(e: KeyboardEvent) {
    e.preventDefault();
    const spec = results[activeResult];
    if (spec) pick(spec.type);
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
    const { node, replaced } = addStep(flow, { type, at: { x: snap(p.x), y: snap(p.y) }, placement: selectedPlacement(flow, selectedId), chatMessage: t('flows.board.defaultChatMessage') });
    if (replaced) announce(t('flows.board.triggerReplaced'));
    showAdded(node);
  }

  export function announce(message: string) {
    note = '';
    void tick().then(() => (note = message));
  }

  export function focusNode(id: NodeId) {
    void tick().then(() => nodeEls[id]?.querySelector<HTMLElement>('[role="button"]')?.focus({ preventScroll: true }));
  }

  export function revealNode(id: NodeId) {
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

  function selectAndShow(id: NodeId) {
    selectedId = id;
    revealNode(id);
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
    revealNode(n.id);
    return true;
  }

  export function removeSelected(): boolean {
    if (selectedEdge && flow.edges.some((edge) => edge.id === selectedEdge)) {
      deleteEdge(selectedEdge);
      return true;
    }
    return selectedId ? removeStep(selectedId) : false;
  }

  export function removeStep(id: NodeId): boolean {
    const { parent, count } = deleteStep(flow, id);
    if (!count) return false;
    selectedEdge = null;
    selectedId = parent;
    if (autoTidy) runTidy(true);
    announce(count === 1 ? t('flows.board.stepRemoved') : t('flows.board.stepsRemoved', { n: count }));
    onremoved?.(count);
    if (parent) selectAndShow(parent);
    else void tick().then(() => startEl?.focus({ preventScroll: true }));
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
    const anchor = cards[n.id]?.addButton(port);
    if (!anchor) return false;
    menuByKey = true;
    addFromPort(n.id, port, anchor);
    return true;
  }

  export function dismissMenu(): boolean {
    if (menu) { closeMenu(true); return true; }
    if (wire) { wire = null; wireTarget = null; mode = 'idle'; return true; }
    if (connecting) { connecting = null; return true; }
    if (selectedEdge) { selectedEdge = null; return true; }
    return false;
  }

  export function zoomBy(factor: number) {
    zoomCenter(factor);
  }

  export function settle(retidy: boolean) {
    if (retidy && autoTidy) runTidy(true);
    else finishTween();
  }

  function resetHostScroll() {
    if (host) { host.scrollLeft = 0; host.scrollTop = 0; }
  }

  function selectEdge(id: Flow['edges'][number]['id']) {
    resetHostScroll();
    selectedEdge = id;
    selectedId = null;
    hoverEdge = null;
    void tick().then(() => host?.querySelector<HTMLButtonElement>('.edge-toolbar button')?.focus({ preventScroll: true }));
  }

  function deleteEdge(id: Flow['edges'][number]['id']) {
    removeEdge(flow, id);
    selectedEdge = null;
    hoverEdge = null;
    announce(t('flows.noteDisconnected'));
    if (autoTidy) runTidy(true);
  }

  const dotGap = $derived(GRID * view.k * (view.k < 0.6 ? 2 : 1));

  const edges = $derived(edgePaths(flow, disp));
  const returns = $derived(loopPaths(flow, disp));

  const stopId = $derived(selectedId && nodeById(selectedId) ? selectedId : (readingOrder(flow)[0]?.id ?? null));

  const triggers = $derived(flow.nodes.filter((n) => specFor(n.type)?.kind === 'trigger'));

  const startWires = $derived(startPaths(flow, disp, { x: startAt.x + START_W, y: startAt.y + START_H / 2 }));

  const wireD = $derived(wirePreview(flow, disp, wire, wireTarget && targetFor(wireTarget) ? wireTarget : null));

  const results = $derived(searchSteps(t, query, ['reply', 'condition']));
  const occupied = $derived(portOccupied(flow, menu?.link));
  const activeResultId = $derived(results[activeResult] ? `${pickerId}-${results[activeResult].type}` : undefined);

  $effect(() => {
    menuSearch?.focus({ preventScroll: true });
  });

  $effect(() => {
    const id = activeResultId;
    if (id) void tick().then(() => document.getElementById(id)?.scrollIntoView({ block: 'nearest' }));
  });


</script>

<div
  class="canvas bb-dot-canvas"
  class:dragging
  class:view-open={viewOpen}
  class:touch
  class:connecting={connecting !== null}
  style:background-size="{dotGap}px {dotGap}px"
  style:background-position="{view.x}px {view.y}px"
  bind:this={host}
  role="application"
  aria-label={t('flows.board.boardLabel')}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  onpointercancel={onPointerUp}
  ondragover={onDragOver}
  ondrop={onDrop}
>
  <div class="world" style:transform="translate({view.x}px, {view.y}px) scale({view.k})" style:--inv={touch ? 1 / shownK : undefined}>
    <svg class="wires" width="1" height="1">
      <defs>
        <marker id="flow-arrow-yes" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto">
          <path d="M0,0 L10,5 L0,10 z" style="fill: rgba(var(--bb-green-glow-rgb), 0.7)" />
        </marker>
        <marker id="flow-arrow-no" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto">
          <path d="M0,0 L10,5 L0,10 z" style="fill: rgba(var(--bb-muted-rgb), 0.6)" />
        </marker>
        <marker id="flow-arrow-preview" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="9" markerHeight="9" markerUnits="userSpaceOnUse" orient="auto">
          <path d="M0,0 L10,5 L0,10 z" fill="var(--bb-green-glow)" />
        </marker>
      </defs>
      {#each startWires as wire (wire.id)}
        <path
          class="wire"
          aria-hidden="true"
          class:taken={trace?.nodes.has(wire.id)}
          class:off={trace && !trace.nodes.has(wire.id)}
          d={wire.d}
          marker-end="url(#flow-arrow-yes)"
        />
      {/each}
      {#each returns as back (back.id)}
        <path aria-hidden="true" class="wire return" d={back.d} marker-end="url(#flow-arrow-no)" />
      {/each}
      {#each edges as edge (edge.id)}
        <path
          class="wire"
          aria-hidden="true"
          class:hot={hoverEdge === edge.id || selectedEdge === edge.id}
          class:chosen={selectedEdge === edge.id}
          class:no={edge.tone === 'no'}
          class:taken={trace?.edges.has(edge.id)}
          class:off={trace && !trace.edges.has(edge.id)}
          d={edge.d}
          marker-end="url(#flow-arrow-{edge.tone})"
        />
        <path
          class="hit"
          d={edge.d}
          bind:this={edgeEls[edge.id]}
          role="button"
          tabindex="0"
          aria-label={t('flows.board.connectionSelection')}
          onfocus={resetHostScroll}
          onkeydown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              e.stopPropagation();
              selectEdge(edge.id);
            }
          }}
          onpointerenter={(e) => { if (e.pointerType === 'mouse') hoverEdge = edge.id; }}
          onpointerleave={(e) => { if (e.pointerType === 'mouse') hoverEdge = null; }}
        />
      {/each}
      {#if wire}
        <path aria-hidden="true" class="wire preview" class:invalid={wireTarget && !targetFor(wireTarget)} d={wireD} marker-end="url(#flow-arrow-preview)" />
      {/if}
    </svg>

    <FlowStart x={startAt.x} y={startAt.y} empty={triggers.length === 0} {touch} bind:el={startEl} onadd={onstart} />

    {#each flow.nodes as node (node.id)}
      <div
        class="slot"
        class:taken={trace?.nodes.has(node.id)}
        class:off={trace && !trace.nodes.has(node.id)}
        class:running={trace?.current === node.id}
        class:source={(connecting ?? wire)?.from === node.id}
        class:target={(connecting && targetFor(node.id)) || (wireTarget === node.id && targetFor(node.id))}
        class:invalid-target={wireTarget === node.id && !targetFor(node.id)}
        bind:this={nodeEls[node.id]}
        style:transform="translate({at(node.id).x}px, {at(node.id).y}px)"
        style:z-index={selectedId === node.id || trace?.current === node.id ? 2 : 1}
        role="presentation"
        onfocusin={(e) => {
          if (!(e.target as HTMLElement).closest('button')) { selectedId = node.id; selectedEdge = null; }
          resetHostScroll();
          if (mode === 'idle' && (e.target as HTMLElement).matches(':focus-visible')) revealNode(node.id);
        }}
      >
        <FlowNodeCard bind:this={cards[node.id]} {node} parallel={parallelPorts(flow, node.id)} selected={selectedId === node.id} tabbable={stopId === node.id} {touch} connecting={connecting !== null} onadd={(port, el) => addFromPort(node.id, port, el)} />
      </div>
    {/each}
  </div>

  {#if menu}
    <span class="menu-anchor" bind:this={menuAnchor} style:left="{menu.sx}px" style:top="{menu.sy}px"></span>
    <PickerPanel bind:open={menuOpen} anchor={menuAnchor} label={t('flows.board.addStep')} width={340} maxHeight={380} onClose={() => closeMenu(true)}>
      <div class="menu" role="presentation" onkeydown={onMenuKey}>
        <SearchInput bind:element={menuSearch} value={query} onValueChange={(v) => { query = v; activeResult = 0; }} placeholder={t('flows.board.searchSteps')} aria-label={t('flows.board.searchSteps')} role="combobox" aria-expanded="true" aria-autocomplete="list" aria-controls="{pickerId}-results" aria-activedescendant={activeResultId} fill />
        {#if occupied}
          <div class="insert-mode" role="group" aria-label={t('flows.board.addStep')}>
            <Button block variant={insertMode === 'insert' ? 'primary' : 'ghost'} aria-pressed={insertMode === 'insert'} onclick={() => (insertMode = 'insert')}><bb-fit>{t('flows.board.insertHere')}</bb-fit></Button>
            <Button block variant={insertMode === 'parallel' ? 'primary' : 'ghost'} aria-pressed={insertMode === 'parallel'} onclick={() => (insertMode = 'parallel')}><bb-fit>{t('flows.board.runParallel')}</bb-fit></Button>
          </div>
        {/if}
        {#if menu.link && flow.nodes.some((node) => targetFor(node.id, menu?.link))}
          <PickerOption onclick={connectFromMenu}>
            <span class="menu-row">
              <IconTile class="menu-tile"><Icon name="branch" size={16} /></IconTile>
              <span class="menu-name">{t('flows.connectExisting')}</span>
            </span>
          </PickerOption>
        {/if}
        <div id="{pickerId}-results" role="listbox" aria-label={t('flows.board.searchSteps')}>
          {#each results as s, index (s.type)}
            {#if !query.trim() && results[index - 1]?.kind !== s.kind}
              <div class="menu-title" role="presentation">{s.kind === 'reply' ? t('flows.board.do') : t('flows.board.onlyIf')}</div>
            {/if}
            <PickerOption id="{pickerId}-{s.type}" role="option" aria-selected={activeResult === index} selected={activeResult === index} tabindex={-1} onclick={() => pick(s.type)}>
              <span class="menu-row" style:--tint={MENU_TINT[s.kind].color} style:--tint-rgb={MENU_TINT[s.kind].rgb}>
                <IconTile class="menu-tile"><Icon name={s.icon} size={16} /></IconTile>
                <span class="menu-text">
                  <span class="menu-name">{stepLabel(t, s)}</span>
                  <span class="menu-sum">{stepSummary(t, s)}</span>
                </span>
              </span>
            </PickerOption>
          {/each}
        </div>
        {#if results.length === 0}
          <span class="menu-sum menu-empty">{t('flows.board.noStepsMatch')}</span>
          <Button variant="ghost" onclick={() => { query = ''; activeResult = 0; menuSearch?.focus(); }}>{t('flows.board.clearSearch')}</Button>
        {/if}
      </div>
    </PickerPanel>
  {/if}

  {#if selectedEdge && flow.edges.some((edge) => edge.id === selectedEdge)}
    <div class="edge-toolbar" bind:this={edgeToolbarEl} style:bottom="{toolbarBottom}px" style:left="calc(50% - {room / 2}px)">
      <Button variant="ghost" onclick={() => selectedEdge && deleteEdge(selectedEdge)}>
        <Icon name="trash" size={16} />
        {t('flows.board.removeConnection')}
      </Button>
    </div>
  {/if}

  {#if connecting}
    <div class="connect-hint" bind:this={hintEl} role="status">
      <span>{t('flows.connectHint')}</span>
      <Button variant="ghost" onclick={() => (connecting = null)}>{t('flows.connectCancel')}</Button>
    </div>
  {/if}

  <button class="view-toggle" type="button" aria-expanded={viewOpen} onclick={() => (viewOpen = !viewOpen)}>
    <Icon name="settings" size={22} />
    <span>{t('flows.viewTools')}</span>
  </button>

  <div class="zoom" bind:this={zoomEl}>
    <IconButton label={t('flows.board.undo')} size="sm" tooltip disabled={!canUndo} onclick={onundo}><Icon name="rotateCcw" size={14} /></IconButton>
    <IconButton label={t('flows.board.redo')} size="sm" tooltip disabled={!canRedo} onclick={onredo}><Icon name="rotateCw" size={14} /></IconButton>
    <Cluster gap={2} align="stretch" nowrap>
      <IconButton label={t('flows.board.zoomOut')} size="sm" tooltip onclick={() => zoomCenter(1 / 1.2)}><Icon name="minus" size={14} /></IconButton>
      <span class="zoom-pct">{Math.round(shownK * 100)}%</span>
      <IconButton label={t('flows.board.zoomIn')} size="sm" tooltip onclick={() => zoomCenter(1.2)}><Icon name="plus" size={14} /></IconButton>
      <IconButton label={t('flows.board.fitView')} size="sm" tooltip onclick={() => fitView()}><Icon name="expand" size={14} /></IconButton>
    </Cluster>
    <span class="zoom-gap" aria-hidden="true"></span>
    <IconButton label={t('flows.board.cleanUp')} size="sm" tooltip onclick={tidyNow}><Icon name="tidy" size={14} /></IconButton>
    <label class="auto">
      <span>{t('flows.board.autoTidy')}</span>
      <Switch bind:checked={autoTidy} label={t('flows.board.autoTidy')} />
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
  .wire.return {
    stroke: rgba(var(--bb-muted-rgb), 0.6);
    stroke-dasharray: 2 6;
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
  .wire.chosen { stroke-width: 3.5; filter: drop-shadow(0 0 5px rgba(var(--bb-green-glow-rgb), 0.45)); }
  .wire.preview { stroke: var(--bb-green-glow); stroke-width: 2.5; filter: drop-shadow(0 0 5px rgba(var(--bb-green-glow-rgb), 0.35)); }
  .wire.preview.invalid { opacity: 0.45; }
  .hit {
    fill: none;
    stroke: transparent;
    stroke-width: 16;
    pointer-events: stroke;
  }
  .hit:focus-visible { stroke: rgba(var(--bb-green-glow-rgb), 0.3); outline: none; }
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
  .slot.taken :global(.node) {
    outline: 1px solid rgba(var(--bb-white-rgb), 0.4);
    outline-offset: 2px;
  }
  .slot.running :global(.node) {
    box-shadow:
      0 0 0 2px var(--bb-green-glow),
      0 0 28px rgba(var(--bb-green-glow-rgb), 0.35);
  }
  .edge-toolbar {
    --btn-radius: var(--bb-radius-pill);
    --btn-min-h: 38px;
    position: absolute;
    left: 50%;
    bottom: calc(var(--flow-dock-room, 0px) + 20px);
    translate: -50% 0;
    z-index: 6;
    padding: 5px;
    border: 1px solid rgba(var(--bb-white-rgb), 0.14);
    border-radius: 999px;
    background: var(--bb-bg-1);
    box-shadow: 0 8px 28px rgba(var(--bb-shadow-rgb), 0.55);
  }
  .canvas.touch .edge-toolbar { --btn-min-h: 44px; }
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
  .insert-mode {
    display: grid;
    grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.2fr);
    gap: 4px;
    --btn-pad: 0 8px;
    --btn-justify: center;
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
  .canvas.connecting .slot {
    opacity: 0.35;
  }
  .canvas.connecting .slot.source,
  .canvas.connecting .slot.target {
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
  .slot.invalid-target :global(.node) { opacity: 0.55; }
  .canvas.touch .hit {
    stroke-width: calc(32px * var(--inv, 1));
  }
  .canvas.touch .slot::before {
    content: '';
    position: absolute;
    top: 50%;
    left: 0;
    right: 0;
    height: max(100%, 44px * var(--inv, 1));
    translate: 0 -50%;
  }
  .canvas.touch .zoom :global(.bb-btn--icon),
  .canvas.touch .connect-hint :global(button) {
    min-width: 44px;
    width: 44px;
    height: 44px;
  }
  .canvas.touch .connect-hint :global(button) {
    width: auto;
  }
  .canvas.touch .auto {
    min-height: 44px;
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    .canvas {
      --flow-dock-room: var(--stage-bottom-room, 0px);
    }
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
