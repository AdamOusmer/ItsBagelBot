<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/dot-canvas.css';
  import CanvasCard from '@bagel/ui/svelte/CanvasCard.svelte';
  import EditorStage from '@bagel/ui/svelte/EditorStage.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { untrack } from 'svelte';
  import { clampLayer } from '../../lib/overlay/clamp';
  import {
    moveBox,
    resizeBox,
    snapTargets,
    type Box,
    type Handle,
    type Snapped,
    type SnapTargets,
  } from '../../lib/overlay/geometry';
  import { LayerHistory } from '../../lib/overlay/history';
  import { KIND_ICON, KIND_LABEL } from '../../lib/overlay/kind';
  import { RESOLUTIONS, RESOLUTION_OPTIONS, pxFromPercent, type Resolution } from '../../lib/overlay/resolutions';
  import { sampleLayers } from '../../lib/overlay/sample-data';
  import type { Layer, LayerKind } from '../../lib/overlay/types';
  import { clampPan, fitCanvas, zoomAround, type View } from '../../lib/overlay/view';
  import LayerContent from './overlay/LayerContent.svelte';
  import LayerInspector from './overlay/LayerInspector.svelte';
  import LayersPanel from './overlay/LayersPanel.svelte';
  import SelectionChrome from './overlay/SelectionChrome.svelte';

  type Gesture =
    | { kind: 'pan'; startX: number; startY: number; view: View }
    | { kind: 'move'; id: string; startX: number; startY: number; box: Box; targets: SnapTargets; moved: boolean }
    | { kind: 'resize'; id: string; handle: Handle; startX: number; startY: number; box: Box; targets: SnapTargets; moved: boolean };

  const BAR_H = 44;
  const SNAP_PX = 6;
  const ZOOM_STEP = 1.2;
  const TIGHT_BAR_W = 620;
  const ADD_KINDS: readonly LayerKind[] = ['text', 'image', 'alert', 'chat'];
  const CARD_KINDS: readonly LayerKind[] = ['image', 'alert', 'chat'];

  let layers = $state<Layer[]>(sampleLayers());
  let selectedId = $state<string | null>(null);
  let resolution = $state<Resolution>('1080p');
  let layersOpen = $state(false);
  let settingsOpen = $state(false);
  let addMenuOpen = $state(false);
  let addAnchorEl = $state<HTMLElement>();
  let hostEl = $state<HTMLElement>();
  let roomW = $state(0);
  let roomH = $state(0);
  let view = $state<View>({ x: 0, y: 0, k: 1 });
  let fitted = $state(true);
  let gesture = $state<Gesture | null>(null);
  let guideX = $state<number | null>(null);
  let guideY = $state<number | null>(null);

  const canvasW = $derived(RESOLUTIONS[resolution].width);
  const canvasH = $derived(RESOLUTIONS[resolution].height);
  const canvasSize = $derived({ width: canvasW, height: canvasH });
  const room = $derived({ width: roomW, height: roomH, top: BAR_H });
  const selectedLayer = $derived(layers.find((l) => l.id === selectedId) ?? null);
  const tightBar = $derived(roomW > 0 && roomW < TIGHT_BAR_W);
  const zoomPct = $derived(Math.round(view.k * 100));
  const busy = $derived(gesture !== null && gesture.kind !== 'pan' && gesture.moved);
  const screenRect = $derived(
    selectedLayer
      ? {
          left: view.x + (selectedLayer.x / 100) * canvasW * view.k,
          top: view.y + (selectedLayer.y / 100) * canvasH * view.k,
          width: (selectedLayer.w / 100) * canvasW * view.k,
          height: (selectedLayer.h / 100) * canvasH * view.k,
        }
      : null,
  );
  const readout = $derived(selectedLayer ? layerReadout(selectedLayer) : '');
  const resolutionOptions = RESOLUTION_OPTIONS.map((value) => ({ value, label: RESOLUTIONS[value].label }));

  // $state.snapshot gives a plain copy: structuredClone in LayerHistory can't clone a Svelte state proxy.
  const history = new LayerHistory(untrack(() => $state.snapshot(layers)));
  let canUndo = $state(false);
  let canRedo = $state(false);

  function layerReadout(layer: Layer): string {
    const x = pxFromPercent(layer.x, canvasW);
    const y = pxFromPercent(layer.y, canvasH);
    const w = pxFromPercent(layer.w, canvasW);
    const h = pxFromPercent(layer.h, canvasH);
    return gesture?.kind === 'resize' ? `${w} x ${h}` : `${x}, ${y}`;
  }

  function syncHistoryFlags(): void {
    canUndo = history.canUndo;
    canRedo = history.canRedo;
  }

  function commit(): void {
    history.commit($state.snapshot(layers));
    syncHistoryFlags();
  }

  function pruneSelection(): void {
    if (selectedId && !layers.some((l) => l.id === selectedId)) selectedId = null;
  }

  function restore(snapshot: Layer[] | undefined | null): void {
    if (!snapshot) return;
    layers = snapshot;
    syncHistoryFlags();
    pruneSelection();
  }

  const undo = () => restore(history.undo());
  const redo = () => restore(history.redo());

  function fit(): void {
    fitted = true;
    if (roomW > 0) view = fitCanvas(room, canvasSize);
  }

  function zoomCenter(factor: number): void {
    fitted = false;
    view = clampPan(zoomAround(view, factor, roomW / 2, (roomH + BAR_H) / 2), room, canvasSize);
  }

  function setResolution(next: Resolution): void {
    resolution = next;
    fit();
  }

  function syncRoom(host: HTMLElement): void {
    roomW = host.clientWidth;
    roomH = host.clientHeight;
    if (fitted) fit();
    else view = clampPan(view, room, canvasSize);
  }

  function onWheel(event: WheelEvent): void {
    event.preventDefault();
    if (event.ctrlKey || event.metaKey) {
      const bounds = hostEl!.getBoundingClientRect();
      fitted = false;
      view = zoomAround(view, Math.exp(-event.deltaY * 0.01), event.clientX - bounds.left, event.clientY - bounds.top);
      view = clampPan(view, room, canvasSize);
      return;
    }
    if (fitted) return;
    view = clampPan({ k: view.k, x: view.x - event.deltaX, y: view.y - event.deltaY }, room, canvasSize);
  }

  $effect(() => {
    const host = hostEl;
    if (!host) return;
    const observer = new ResizeObserver(() => syncRoom(host));
    observer.observe(host);
    untrack(() => syncRoom(host));
    host.addEventListener('wheel', onWheel, { passive: false });
    return () => {
      observer.disconnect();
      host.removeEventListener('wheel', onWheel);
    };
  });

  function openSettings(id: string): void {
    selectedId = id;
    settingsOpen = true;
  }

  function addLayer(kind: LayerKind): void {
    const base = {
      id: crypto.randomUUID(),
      name: `New ${KIND_LABEL[kind].toLowerCase()}`,
      x: 35,
      y: 35,
      w: 30,
      h: 20,
      opacity: 100,
    };
    const layerByKind: Record<LayerKind, () => Layer> = {
      text: () => ({ ...base, kind: 'text', content: 'New sample text', fontSize: 7, color: '#f6efe1', align: 'start' }),
      image: () => ({ ...base, kind: 'image', caption: 'Sample image placeholder', fit: 'contain' }),
      alert: () => ({ ...base, kind: 'alert', title: 'Sample alert title', subtitle: 'Sample alert subtitle', tone: 'free' }),
      chat: () => ({ ...base, kind: 'chat', rows: 3, messages: [] }),
    };
    const layer = layerByKind[kind]();
    layers.push(layer);
    selectedId = layer.id;
    addMenuOpen = false;
    commit();
  }

  function removeLayer(id: string): void {
    const index = layers.findIndex((l) => l.id === id);
    if (index === -1) return;
    layers.splice(index, 1);
    pruneSelection();
    commit();
  }

  function duplicateLayer(id: string): void {
    const index = layers.findIndex((l) => l.id === id);
    if (index === -1) return;
    const copy = $state.snapshot(layers[index]);
    copy.id = crypto.randomUUID();
    copy.name = `${copy.name} copy`;
    copy.x = Math.min(100 - copy.w, copy.x + 4);
    copy.y = Math.min(100 - copy.h, copy.y + 4);
    layers.splice(index + 1, 0, copy);
    selectedId = copy.id;
    commit();
  }

  function moveLayer(id: string, direction: 1 | -1): void {
    const index = layers.findIndex((l) => l.id === id);
    const target = index + direction;
    if (index === -1 || target < 0 || target >= layers.length) return;
    [layers[index], layers[target]] = [layers[target], layers[index]];
    commit();
  }

  function boxOf(layer: Layer): Box {
    return { x: layer.x, y: layer.y, w: layer.w, h: layer.h };
  }

  function onLayerPointerDown(event: PointerEvent, layer: Layer): void {
    if (event.button !== 0) return;
    event.stopPropagation();
    selectedId = layer.id;
    gesture = {
      kind: 'move',
      id: layer.id,
      startX: event.clientX,
      startY: event.clientY,
      box: boxOf(layer),
      targets: snapTargets(layers, layer.id),
      moved: false,
    };
  }

  function onHandleDown(event: PointerEvent, handle: Handle): void {
    if (!selectedLayer) return;
    event.stopPropagation();
    gesture = {
      kind: 'resize',
      id: selectedLayer.id,
      handle,
      startX: event.clientX,
      startY: event.clientY,
      box: boxOf(selectedLayer),
      targets: snapTargets(layers, selectedLayer.id),
      moved: false,
    };
  }

  function onWorkspacePointerDown(event: PointerEvent): void {
    selectedId = null;
    if (fitted) return;
    gesture = { kind: 'pan', startX: event.clientX, startY: event.clientY, view: { ...view } };
  }

  function applyBox(id: string, result: Snapped): void {
    const layer = layers.find((l) => l.id === id);
    if (!layer) return;
    Object.assign(layer, result.box);
    guideX = result.guideX;
    guideY = result.guideY;
  }

  function onWindowPointerMove(event: PointerEvent): void {
    if (!gesture) return;
    const screenDx = event.clientX - gesture.startX;
    const screenDy = event.clientY - gesture.startY;
    if (gesture.kind === 'pan') {
      view = clampPan({ k: view.k, x: gesture.view.x + screenDx, y: gesture.view.y + screenDy }, room, canvasSize);
      return;
    }
    const scaleX = 100 / (canvasW * view.k);
    const scaleY = 100 / (canvasH * view.k);
    const threshold = { x: SNAP_PX * scaleX, y: SNAP_PX * scaleY };
    const dx = screenDx * scaleX;
    const dy = screenDy * scaleY;
    gesture.moved = gesture.moved || Math.hypot(screenDx, screenDy) > 2;
    if (!gesture.moved) return;
    const result =
      gesture.kind === 'move'
        ? moveBox(gesture.box, dx, dy, gesture.targets, threshold)
        : resizeBox(gesture.box, gesture.handle, dx, dy, gesture.targets, threshold);
    applyBox(gesture.id, result);
  }

  function onWindowPointerUp(): void {
    if (gesture && gesture.kind !== 'pan' && gesture.moved) commit();
    gesture = null;
    guideX = null;
    guideY = null;
  }

  function onLayerKeyDown(event: KeyboardEvent, layer: Layer): void {
    const step = event.shiftKey ? 5 : 1;
    const nudges: Record<string, () => void> = {
      ArrowLeft: () => (layer.x -= step),
      ArrowRight: () => (layer.x += step),
      ArrowUp: () => (layer.y -= step),
      ArrowDown: () => (layer.y += step),
    };
    if (event.key === 'Delete' || event.key === 'Backspace') {
      event.preventDefault();
      removeLayer(layer.id);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      openSettings(layer.id);
    } else if (nudges[event.key]) {
      event.preventDefault();
      nudges[event.key]();
      clampLayer(layer);
      commit();
    }
  }

  const SHORTCUTS: Record<string, () => void> = {
    z: undo,
    Z: redo,
    y: redo,
    '=': () => zoomCenter(ZOOM_STEP),
    '+': () => zoomCenter(ZOOM_STEP),
    '-': () => zoomCenter(1 / ZOOM_STEP),
    '0': fit,
  };

  function onWindowKeyDown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement | null;
    if (target?.closest('input, textarea, select')) return;
    const action = event.metaKey || event.ctrlKey ? SHORTCUTS[event.key] : undefined;
    if (!action) return;
    event.preventDefault();
    action();
  }
</script>

<svelte:window
  onkeydown={onWindowKeyDown}
  onpointermove={onWindowPointerMove}
  onpointerup={onWindowPointerUp}
  onpointercancel={onWindowPointerUp}
/>

{#snippet topStart()}
  <IconButton label="Layers" tooltip size="sm" aria-pressed={layersOpen} onclick={() => (layersOpen = !layersOpen)}>
    <Icon name="list" size={15} />
  </IconButton>
  <span class="add-anchor" bind:this={addAnchorEl}>
    <IconButton label="Add layer" tooltip size="sm" onclick={() => (addMenuOpen = !addMenuOpen)}>
      <Icon name="plus" size={15} />
    </IconButton>
  </span>
  <PickerPanel bind:open={addMenuOpen} anchor={addAnchorEl} label="Add layer" width={200} maxHeight={260} placement="below">
    {#each ADD_KINDS as kind (kind)}
      <PickerOption onclick={() => addLayer(kind)}>
        <span class="menu-row">
          <Icon name={KIND_ICON[kind]} size={15} />
          <span>{KIND_LABEL[kind]}</span>
        </span>
      </PickerOption>
    {/each}
  </PickerPanel>
  <span class="bar-gap"></span>
  <IconButton label="Undo" tooltip size="sm" disabled={!canUndo} onclick={undo}>
    <Icon name="arrowLeft" size={15} />
  </IconButton>
  <IconButton label="Redo" tooltip size="sm" disabled={!canRedo} onclick={redo}>
    <Icon name="arrowLeft" size={15} class="redo-icon" />
  </IconButton>
{/snippet}

{#snippet topEnd()}
  {#if !tightBar}
    <IconButton label="Zoom out" tooltip size="sm" onclick={() => zoomCenter(1 / ZOOM_STEP)}>
      <Icon name="minus" size={15} />
    </IconButton>
    <span class="zoom-pct">{zoomPct}%</span>
    <IconButton label="Zoom in" tooltip size="sm" onclick={() => zoomCenter(ZOOM_STEP)}>
      <Icon name="plus" size={15} />
    </IconButton>
  {/if}
  <IconButton label="Fit to window" tooltip size="sm" aria-pressed={fitted} onclick={fit}>
    <Icon name="expand" size={15} />
  </IconButton>
  <span class="bar-gap"></span>
  <span class="resolution">
    <Select
      label="Canvas resolution"
      options={resolutionOptions}
      value={resolution}
      fill
      onchange={(event) => setResolution(event.currentTarget.value as Resolution)}
    />
  </span>
{/snippet}

{#snippet leftPanel()}
  <LayersPanel
    {layers}
    {selectedId}
    onSelect={(id) => (selectedId = id)}
    onMoveUp={(id) => moveLayer(id, 1)}
    onMoveDown={(id) => moveLayer(id, -1)}
    onRemove={removeLayer}
    onClose={() => (layersOpen = false)}
  />
{/snippet}

{#snippet rightPanel()}
  {#if selectedLayer}
    <div class="settings">
      <LayerInspector
        layer={selectedLayer}
        {canvasW}
        {canvasH}
        onCommit={commit}
        onRemove={() => removeLayer(selectedLayer.id)}
        onDuplicate={() => duplicateLayer(selectedLayer.id)}
        onClose={() => (settingsOpen = false)}
      />
    </div>
  {/if}
{/snippet}

{#snippet workspace()}
  <div
    class="workspace"
    class:workspace--pannable={!fitted}
    class:workspace--panning={gesture?.kind === 'pan'}
    role="presentation"
    bind:this={hostEl}
    onpointerdown={onWorkspacePointerDown}
  >
    <div
      class="canvas-frame bb-dot-canvas"
      style:left="{view.x}px"
      style:top="{view.y}px"
      style:width="{canvasW * view.k}px"
      style:height="{canvasH * view.k}px"
    ></div>
    <div
      class="canvas"
      style:width="{canvasW}px"
      style:height="{canvasH}px"
      style:transform="translate({view.x}px, {view.y}px) scale({view.k})"
      style:--canvas-scale={canvasH / 360}
      style:--canvas-card-zoom-inv={1 / view.k}
    >
      {#each layers as layer (layer.id)}
        <CanvasCard
          class="layer layer--{layer.kind}"
          background={CARD_KINDS.includes(layer.kind)}
          selected={layer.id === selectedId}
          style="left: {layer.x}%; top: {layer.y}%; width: {layer.w}%; height: {layer.h}%; opacity: {layer.opacity / 100};"
          role="button"
          tabindex={0}
          aria-label={layer.name}
          aria-pressed={layer.id === selectedId}
          onpointerdown={(event) => onLayerPointerDown(event, layer)}
          ondblclick={() => openSettings(layer.id)}
          onkeydown={(event) => onLayerKeyDown(event, layer)}
        >
          <LayerContent {layer} {canvasH} />
        </CanvasCard>
      {/each}
    </div>

    {#if guideX !== null}
      <span class="guide guide--x" style:left="{view.x + (guideX / 100) * canvasW * view.k}px"></span>
    {/if}
    {#if guideY !== null}
      <span class="guide guide--y" style:top="{view.y + (guideY / 100) * canvasH * view.k}px"></span>
    {/if}

    {#if selectedLayer && screenRect}
      <SelectionChrome
        rect={screenRect}
        roomWidth={roomW}
        topLimit={BAR_H + 8}
        {busy}
        {readout}
        {onHandleDown}
        onSettings={() => openSettings(selectedLayer.id)}
        onDuplicate={() => duplicateLayer(selectedLayer.id)}
        onRemove={() => removeLayer(selectedLayer.id)}
      />
    {/if}
  </div>
{/snippet}

<EditorStage
  slimBar
  --editor-panel-left="280px"
  --editor-panel-right="300px"
  label="Overlay canvas"
  {topStart}
  {topEnd}
  leftPanel={layersOpen ? leftPanel : undefined}
  rightPanel={settingsOpen && selectedLayer ? rightPanel : undefined}
  children={workspace}
/>

<style>
  :global(.redo-icon) {
    transform: scaleX(-1);
  }

  .workspace {
    position: absolute;
    inset: 0;
    overflow: hidden;
    background: var(--bb-bg-1);
  }

  .workspace--pannable {
    cursor: grab;
  }

  .workspace--panning {
    cursor: grabbing;
  }

  .canvas-frame {
    position: absolute;
    box-shadow: 0 0 0 1px rgba(var(--bb-white-rgb), 0.1);
  }

  .canvas {
    position: absolute;
    top: 0;
    left: 0;
    transform-origin: 0 0;
    pointer-events: none;
  }

  :global(.layer) {
    position: absolute;
    overflow: hidden;
    cursor: grab;
    pointer-events: auto;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }

  :global(.layer--text) {
    display: flex;
    align-items: center;
    padding: 0 8px;
    box-sizing: border-box;
  }

  .guide {
    position: absolute;
    z-index: 2;
    background: var(--bb-green-glow);
    pointer-events: none;
  }

  .guide--x {
    top: 44px;
    bottom: 0;
    width: 1px;
  }

  .guide--y {
    left: 0;
    right: 0;
    height: 1px;
  }

  .settings {
    height: 100%;
    overflow: hidden;
    border-radius: var(--bb-radius-md);
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
    pointer-events: auto;
  }

  .bar-gap {
    flex: none;
    width: 10px;
  }

  .add-anchor {
    position: relative;
    display: inline-flex;
  }

  .menu-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .zoom-pct {
    min-width: 38px;
    color: rgba(var(--bb-white-rgb), 0.72);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    text-align: center;
  }

  .resolution {
    flex: none;
    display: inline-flex;
    width: 104px;
  }
</style>
