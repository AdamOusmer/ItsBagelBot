<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '@bagel/ui/styles/elements/dot-canvas.css';
  import CanvasCard from '@bagel/ui/svelte/CanvasCard.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import Divider from '@bagel/ui/svelte/Divider.svelte';
  import EditorStage from '@bagel/ui/svelte/EditorStage.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Select from '@bagel/ui/svelte/Select.svelte';
  import { untrack } from 'svelte';
  import { clampLayer } from '../../lib/overlay/clamp';
  import { LayerHistory } from '../../lib/overlay/history';
  import { KIND_ICON, KIND_LABEL } from '../../lib/overlay/kind';
  import { RESOLUTIONS, RESOLUTION_OPTIONS, type Resolution } from '../../lib/overlay/resolutions';
  import { sampleLayers } from '../../lib/overlay/sample-data';
  import type { Layer, LayerKind } from '../../lib/overlay/types';
  import LayerInspector from './overlay/LayerInspector.svelte';
  import LayersPanel from './overlay/LayersPanel.svelte';

  const PAD = 12;
  // 44px bar + 12px gap.
  const BAR_RESERVE = 56;
  const PANEL_W = 280;
  const ADD_KINDS: readonly LayerKind[] = ['text', 'image', 'alert', 'chat'];

  let layers = $state<Layer[]>(sampleLayers());
  let selectedId = $state<string | null>(null);
  let resolution = $state<Resolution>('1080p');
  let stageFrameEl = $state<HTMLElement>();
  let stageW = $state(640);
  let stageH = $state(360);

  const CANVAS_W = $derived(RESOLUTIONS[resolution].width);
  const CANVAS_H = $derived(RESOLUTIONS[resolution].height);
  const resolutionOptions = RESOLUTION_OPTIONS.map((value) => ({ value, label: RESOLUTIONS[value].label }));

  // $state.snapshot gives a plain, detached copy: structuredClone (inside LayerHistory)
  // can't clone a Svelte state proxy directly.
  const history = new LayerHistory(untrack(() => $state.snapshot(layers)));
  let canUndo = $state(false);
  let canRedo = $state(false);

  const selectedLayer = $derived(layers.find((l) => l.id === selectedId) ?? null);

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

  function undo(): void {
    const snapshot = history.undo();
    if (!snapshot) return;
    layers = snapshot;
    syncHistoryFlags();
    pruneSelection();
  }

  function redo(): void {
    const snapshot = history.redo();
    if (!snapshot) return;
    layers = snapshot;
    syncHistoryFlags();
    pruneSelection();
  }

  let panelOpen = $state(false);
  const TIGHT_BAR_W = 420;
  const COMPACT_READOUT_W = 300;
  let tightBar = $state(false);
  let compactReadout = $state(false);

  function select(id: string | null): void {
    selectedId = id;
  }

  function openSettings(id: string): void {
    selectedId = id;
    panelOpen = true;
  }

  function toggleLayers(): void {
    if (panelOpen && !selectedLayer) {
      panelOpen = false;
      return;
    }
    selectedId = null;
    panelOpen = true;
  }

  function closePanel(): void {
    panelOpen = false;
  }

  const stageScale = $derived(stageW / CANVAS_W);
  const previewPct = $derived(Math.round(stageScale * 100));
  const ringInvScale = $derived(stageScale > 0 ? 1 / stageScale : 1);
  const CARD_KINDS: readonly LayerKind[] = ['image', 'alert'];
  // Layer chrome (alert/chat/image text, paddings, icons) was tuned against a
  // 360px-tall reference canvas; this keeps it the same proportion of the
  // frame at every resolution instead of shrinking as the canvas grows.
  const canvasScale = $derived(CANVAS_H / 360);
  const captionIconSize = $derived(Math.round(22 * canvasScale));
  const readoutText = $derived(
    compactReadout ? `${previewPct}%` : `${CANVAS_W} x ${CANVAS_H}, ${previewPct}%`,
  );

  function fitStage(frame: HTMLElement): void {
    const availW = Math.max(0, frame.clientWidth - PAD * 2);
    const availH = Math.max(0, frame.clientHeight - BAR_RESERVE - PAD);
    let w = availW;
    let h = (w * 9) / 16;
    if (h > availH) {
      h = availH;
      w = (h * 16) / 9;
    }
    stageW = Math.round(w);
    stageH = Math.round(h);
  }

  function syncLayout(frame: HTMLElement): void {
    tightBar = frame.clientWidth < TIGHT_BAR_W;
    compactReadout = frame.clientWidth < COMPACT_READOUT_W;
    fitStage(frame);
  }

  $effect(() => {
    const frame = stageFrameEl;
    if (!frame) return;
    const observer = new ResizeObserver(() => syncLayout(frame));
    observer.observe(frame);
    syncLayout(frame);
    return () => observer.disconnect();
  });

  let addMenuOpen = $state(false);
  let addAnchorEl = $state<HTMLElement>();

  function pickKind(kind: LayerKind): void {
    addLayer(kind);
    addMenuOpen = false;
  }

  function addLayer(kind: LayerKind): void {
    const base = {
      id: crypto.randomUUID(),
      name: `New ${KIND_LABEL[kind].toLowerCase()}`,
      x: 30,
      y: 30,
      w: 30,
      h: 20,
      opacity: 100,
    };
    let layer: Layer;
    if (kind === 'text') {
      layer = { ...base, kind, content: 'New sample text', fontSize: 7, color: '#f6efe1', align: 'start' };
    } else if (kind === 'image') {
      layer = { ...base, kind, caption: 'Sample image placeholder', fit: 'contain' };
    } else if (kind === 'alert') {
      layer = { ...base, kind, title: 'Sample alert title', subtitle: 'Sample alert subtitle', tone: 'free' };
    } else {
      layer = { ...base, kind, rows: 3, messages: [] };
    }
    layers.push(layer);
    openSettings(layer.id);
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

  let dragId: string | null = null;
  let dragStartX = 0;
  let dragStartY = 0;
  let dragLayerX = 0;
  let dragLayerY = 0;
  let dragRect: DOMRect | null = null;
  let dragMoved = false;

  function onLayerPointerDown(event: PointerEvent, layer: Layer): void {
    select(layer.id);
    const stage = (event.currentTarget as HTMLElement).parentElement;
    if (!stage) return;
    dragId = layer.id;
    dragStartX = event.clientX;
    dragStartY = event.clientY;
    dragLayerX = layer.x;
    dragLayerY = layer.y;
    dragRect = stage.getBoundingClientRect();
    dragMoved = false;
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  }

  function onLayerPointerMove(event: PointerEvent): void {
    if (!dragId || !dragRect || dragRect.width === 0 || dragRect.height === 0) return;
    const layer = layers.find((l) => l.id === dragId);
    if (!layer) return;
    const dxPct = ((event.clientX - dragStartX) / dragRect.width) * 100;
    const dyPct = ((event.clientY - dragStartY) / dragRect.height) * 100;
    if (Math.abs(dxPct) > 0.1 || Math.abs(dyPct) > 0.1) dragMoved = true;
    layer.x = dragLayerX + dxPct;
    layer.y = dragLayerY + dyPct;
    clampLayer(layer);
  }

  function onLayerPointerUp(): void {
    if (dragMoved) commit();
    dragId = null;
    dragRect = null;
  }

  function onLayerKeyDown(event: KeyboardEvent, layer: Layer): void {
    const step = event.shiftKey ? 5 : 1;
    let handled = true;
    if (event.key === 'ArrowLeft') layer.x -= step;
    else if (event.key === 'ArrowRight') layer.x += step;
    else if (event.key === 'ArrowUp') layer.y -= step;
    else if (event.key === 'ArrowDown') layer.y += step;
    else if (event.key === 'Delete' || event.key === 'Backspace') removeLayer(layer.id);
    else if (event.key === 'Enter') openSettings(layer.id);
    else handled = false;
    if (!handled) return;
    event.preventDefault();
    if (event.key.startsWith('Arrow')) {
      clampLayer(layer);
      commit();
    }
  }

  function onStagePointerDown(event: PointerEvent): void {
    if (event.target === event.currentTarget) selectedId = null;
  }

  function onWindowKeyDown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement | null;
    if (target?.closest('input, textarea, select')) return;
    const mod = event.metaKey || event.ctrlKey;
    if (!mod) return;
    if (event.key.toLowerCase() === 'z' && !event.shiftKey) {
      event.preventDefault();
      undo();
    } else if (event.key.toLowerCase() === 'z' && event.shiftKey) {
      event.preventDefault();
      redo();
    } else if (event.key.toLowerCase() === 'y') {
      event.preventDefault();
      redo();
    }
  }
</script>

<svelte:window onkeydown={onWindowKeyDown} />

{#snippet topStart()}
  <IconButton label="Layers" tooltip size="sm" aria-pressed={panelOpen} onclick={toggleLayers}>
    <Icon name="list" size={15} />
  </IconButton>
  <span class="add-anchor" bind:this={addAnchorEl}>
    <IconButton label="Add layer" tooltip size="sm" onclick={() => (addMenuOpen = !addMenuOpen)}>
      <Icon name="plus" size={15} />
    </IconButton>
  </span>
  <PickerPanel bind:open={addMenuOpen} anchor={addAnchorEl} label="Add layer" width={200} maxHeight={260} placement="below">
    {#each ADD_KINDS as kind (kind)}
      <PickerOption onclick={() => pickKind(kind)}>
        <span class="menu-row">
          <Icon name={KIND_ICON[kind]} size={15} />
          <span>{KIND_LABEL[kind]}</span>
        </span>
      </PickerOption>
    {/each}
  </PickerPanel>
{/snippet}

{#snippet topCenter()}
  {#if !tightBar}
    <IconButton label="Undo" tooltip size="sm" disabled={!canUndo} onclick={undo}>
      <Icon name="arrowLeft" size={15} />
    </IconButton>
    <IconButton label="Redo" tooltip size="sm" disabled={!canRedo} onclick={redo}>
      <Icon name="arrowLeft" size={15} class="toolbar-redo-icon" />
    </IconButton>
    <Divider vertical />
  {/if}
  <span class="resolution-select">
    <Select
      label="Canvas resolution"
      options={resolutionOptions}
      value={resolution}
      fill
      onchange={(e) => (resolution = e.currentTarget.value as Resolution)}
    />
  </span>
{/snippet}

{#snippet topEnd()}
  <span class="stage-readout">{readoutText}</span>
{/snippet}

{#snippet leftPanel()}
  {#if selectedLayer}
    <LayerInspector
      layer={selectedLayer}
      canvasW={CANVAS_W}
      canvasH={CANVAS_H}
      onCommit={commit}
      onRemove={() => removeLayer(selectedLayer.id)}
      onDuplicate={() => duplicateLayer(selectedLayer.id)}
      onClose={() => select(null)}
    />
  {:else}
    <LayersPanel
      {layers}
      {selectedId}
      onSelect={select}
      onMoveUp={(id) => moveLayer(id, 1)}
      onMoveDown={(id) => moveLayer(id, -1)}
      onRemove={removeLayer}
      onClose={closePanel}
    />
  {/if}
{/snippet}

{#snippet stageContent()}
  <div class="stage-frame" bind:this={stageFrameEl} style="padding: {BAR_RESERVE}px {PAD}px {PAD}px {PAD}px;">
    <div class="stage bb-dot-canvas" style="width: {stageW}px; height: {stageH}px;">
      <div
        class="stage-canvas"
        role="presentation"
        style="width: {CANVAS_W}px; height: {CANVAS_H}px; transform: scale({stageScale}); --canvas-scale: {canvasScale}; --canvas-card-zoom-inv: {ringInvScale};"
        onpointerdown={onStagePointerDown}
      >
      {#each layers as layer (layer.id)}
        {@const front = layer.id === selectedId}
        <CanvasCard
          class="layer layer--{layer.kind}"
          background={CARD_KINDS.includes(layer.kind)}
          selected={front}
          style="left: {layer.x}%; top: {layer.y}%; width: {layer.w}%; height: {layer.h}%; opacity: {layer.opacity / 100};"
          role="button"
          tabindex={0}
          aria-label={layer.name}
          aria-pressed={front}
          onpointerdown={(e) => onLayerPointerDown(e, layer)}
          onpointermove={onLayerPointerMove}
          onpointerup={onLayerPointerUp}
          ondblclick={() => openSettings(layer.id)}
          onkeydown={(e) => onLayerKeyDown(e, layer)}
        >
          {#if layer.kind === 'text'}
            <span
              class="text-content"
              style="font-size: {(layer.fontSize / 100) * CANVAS_H}px; color: {layer.color}; text-align: {layer.align === 'start' ? 'left' : layer.align === 'end' ? 'right' : 'center'};"
              >{layer.content}</span
            >
          {:else if layer.kind === 'image'}
            <div class="image-placeholder">
              <Icon name="modules" size={captionIconSize} />
              <span>{layer.caption}</span>
            </div>
          {:else if layer.kind === 'alert'}
            <div class="alert-box">
              <Chip tone={layer.tone} pressed as="span">{layer.tone}</Chip>
              <strong class="alert-title">{layer.title}</strong>
              <span class="alert-subtitle">{layer.subtitle}</span>
            </div>
          {:else if layer.kind === 'chat'}
            <div class="chat-box">
              {#each layer.messages.slice(0, layer.rows) as m (m.user)}
                <p class="chat-line">
                  <span class="chat-user">{m.user}</span>
                  <span class="chat-text">{m.message}</span>
                </p>
              {/each}
            </div>
          {/if}
        </CanvasCard>
      {/each}
      </div>
    </div>
  </div>
{/snippet}

<EditorStage
  class="overlay-stage"
  slimBar
  --editor-panel-left={`${PANEL_W}px`}
  label="Overlay stage"
  {topStart}
  {topCenter}
  {topEnd}
  leftPanel={panelOpen ? leftPanel : undefined}
  children={stageContent}
/>

<style>
  :global(.toolbar-redo-icon) {
    transform: scaleX(-1);
  }

  .stage-frame {
    position: relative;
    width: 100%;
    height: 100%;
    min-width: 0;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }

  .stage-readout {
    flex: none;
    min-width: 0;
    overflow: hidden;
    color: rgba(var(--bb-white-rgb), 0.58);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    letter-spacing: 0.02em;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .add-anchor {
    display: inline-flex;
    position: relative;
  }

  /* The ui Select trigger sizes to its own content, not its shrunk flex
     parent; give it a fixed, known-compact width here instead. */
  .resolution-select {
    flex: none;
    display: inline-flex;
    width: 104px;
  }

  .menu-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .stage {
    position: relative;
    flex: none;
    border-radius: var(--bb-radius-lg);
    box-shadow: 0 24px 60px rgba(var(--bb-shadow-rgb), 0.6), inset 0 0 0 1px rgba(var(--bb-white-rgb), 0.08);
    overflow: hidden;
  }

  .stage-canvas {
    position: absolute;
    top: 0;
    left: 0;
    transform-origin: 0 0;
  }

  :global(.layer) {
    position: absolute;
    overflow: hidden;
    cursor: grab;
    touch-action: none;
    -webkit-user-select: none;
    user-select: none;
  }

  :global(.layer.selected) {
    cursor: grabbing;
  }

  :global(.layer--text) {
    display: flex;
    align-items: center;
    padding: 4px 8px;
  }

  .text-content {
    width: 100%;
    font-family: var(--bb-font-body);
    font-weight: 600;
    line-height: 1.2;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .image-placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: calc(var(--canvas-scale, 1) * 6px);
    width: 100%;
    height: 100%;
    color: var(--bb-muted);
    font-size: calc(var(--canvas-scale, 1) * 11px);
    text-align: center;
    padding: calc(var(--canvas-scale, 1) * 6px);
  }

  .alert-box {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    gap: calc(var(--canvas-scale, 1) * 4px);
    width: 100%;
    height: 100%;
    padding: calc(var(--canvas-scale, 1) * 10px) calc(var(--canvas-scale, 1) * 12px);
  }

  .alert-box :global(.bb-chip) {
    font-size: calc(var(--canvas-scale, 1) * 7.5px);
    padding: calc(var(--canvas-scale, 1) * 3px) calc(var(--canvas-scale, 1) * 7px);
    gap: calc(var(--canvas-scale, 1) * 4px);
  }

  .alert-title {
    font-family: var(--bb-font-body);
    font-size: calc(var(--canvas-scale, 1) * 13px);
    font-weight: 700;
    color: var(--bb-white);
  }

  .alert-subtitle {
    font-size: calc(var(--canvas-scale, 1) * 11.5px);
    color: var(--bb-muted);
  }

  .chat-box {
    display: flex;
    flex-direction: column;
    gap: calc(var(--canvas-scale, 1) * 4px);
    width: 100%;
    height: 100%;
    padding: calc(var(--canvas-scale, 1) * 8px) calc(var(--canvas-scale, 1) * 10px);
    background: rgba(var(--bb-black-rgb), 0.55);
    overflow: hidden;
  }

  .chat-line {
    margin: 0;
    /* Smaller reference than the other chrome text: a real chat overlay runs
       dense, and the sample messages need the room to read in full. */
    font-size: calc(var(--canvas-scale, 1) * 6.5px);
    line-height: 1.5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chat-user {
    font-weight: 700;
    color: var(--bb-tan-light);
    margin-right: calc(var(--canvas-scale, 1) * 5px);
  }

  .chat-text {
    color: var(--bb-white);
  }

  /* Bar gaps must stay pointer-events: none (editor-stage.css) or the window drag strip underneath stops working. */
</style>
