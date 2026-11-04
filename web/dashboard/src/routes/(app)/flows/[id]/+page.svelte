<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick, untrack } from 'svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Icon from '#lib/components/flow/FlowIcon.svelte';
  import Tooltip from '@bagel/ui/svelte/Tooltip.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { getI18n } from '@bagel/kit';
  import FlowCanvas, { type FlowCanvasApi, type FlowTrace } from '#lib/components/flow/FlowCanvas.svelte';
  import type { LibraryMode } from '#lib/components/flow/StepLibrary.svelte';
  import { FlowHistory } from '#lib/flow/history';
  import { allowed, resolveShortcut, type Shortcut } from '#lib/flow/keyboard';
  import { addNode } from '#lib/flow/ops';
  import { outputsFor, specFor, type Flow } from '#lib/flow/model';
  import { savedEnabled, savedPreviews } from '#lib/flow/session.svelte';
  import { flowPreview } from '#lib/flow/thumb';
  import { SHEET_QUERY, sheetDrag } from '#lib/flow/sheet';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const TOOLS = [
    { mode: 'trigger', label: 'When', icon: 'bolt' },
    { mode: 'condition', label: 'Only if', icon: 'branch' },
    { mode: 'reply', label: 'Do', icon: 'messageSquare' }
  ] as const;

  const PANEL_ROOM = 392;
  const SHEET_SHARE = 0.56;
  const TALL_SHEET_SHARE = 0.44;
  const TALL_VIEWPORT = 900;
  const SHEET_GAP = 12;

  const loadInspector = () => import('#lib/components/flow/NodeInspector.svelte');
  const loadLibrary = () => import('#lib/components/flow/StepLibrary.svelte');
  const loadTestRun = () => import('#lib/components/flow/TestRunPanel.svelte');

  let flow = $state<Flow>(structuredClone(untrack(() => data.flow)));
  const contentOf = (f: Flow) =>
    JSON.stringify({ name: f.name, edges: f.edges, nodes: f.nodes.map((n) => ({ id: n.id, type: n.type, config: n.config })) });

  let baseline = $state(contentOf(untrack(() => data.flow)));
  let selectedId = $state<string | null>(null);
  let testRunOpen = $state(false);
  let trace = $state<FlowTrace | null>(null);
  let autoTidy = $state(true);
  let canvas = $state<FlowCanvasApi>();
  let libMode = $state<LibraryMode | null>(untrack(() => (data.flow.nodes.length === 0 ? 'trigger' : null)));
  let flyoutEl = $state<HTMLElement>();
  let editorEl = $state<HTMLElement>();
  let barEl = $state<HTMLElement>();
  let railEl = $state<HTMLElement>();
  let boardEl = $state<HTMLElement>();
  let sheetMode = false;
  let inspecting = $state(false);
  let inspectFocus = $state(0);
  let libFocus = $state(0);
  let libReturn: HTMLElement | null = null;
  let runReturn: HTMLElement | null = null;

  const { t } = getI18n();
  const edits = new FlowHistory(untrack(() => flow));
  const dirty = $derived(contentOf(flow) !== baseline);
  const panelRoom = $derived((inspecting && selectedId) || testRunOpen ? PANEL_ROOM : 0);

  $effect(() => {
    edits.sync(flow);
  });

  // iOS shrinks innerHeight under the keyboard; fixed elements stay anchored to this layout viewport.
  const layoutHeight = () => document.documentElement.clientHeight;
  const sheetHeight = () => Math.round(layoutHeight() * (layoutHeight() >= TALL_VIEWPORT ? TALL_SHEET_SHARE : SHEET_SHARE));

  function syncViewport() {
    const style = editorEl?.style;
    if (!style || !sheetMode) return;
    const height = visualViewport?.height ?? layoutHeight();
    const keyboard = Math.max(0, layoutHeight() - height - (visualViewport?.offsetTop ?? 0));
    style.setProperty('--flow-kb', `${keyboard}px`);
    style.setProperty('--flow-vvh', `${height}px`);
    style.setProperty('--flow-sheet-h', `${sheetHeight()}px`);
    style.setProperty('--flow-board-left', `${boardEl?.getBoundingClientRect().left ?? 0}px`);
    if (keyboard === 0 && scrollY !== 0) scrollTo(0, 0);
    const field = document.activeElement;
    if (keyboard > 0 && field?.matches('input, textarea, select') && field.closest('.flyout, .inspector')) field.scrollIntoView({ block: 'center' });
  }

  function viewInsets(withPanel: boolean) {
    if (!sheetMode || !barEl || !railEl || !boardEl) return null;
    const board = boardEl.getBoundingClientRect();
    const bar = barEl.getBoundingClientRect();
    const rail = railEl.getBoundingClientRect();
    const railIsRow = rail.width > rail.height;
    const toolbars = railIsRow ? board.bottom - rail.top : 72;
    return {
      top: bar.bottom - board.top + SHEET_GAP,
      right: 16,
      bottom: (withPanel ? sheetHeight() : toolbars) + SHEET_GAP,
      left: railIsRow ? 16 : rail.right - board.left + SHEET_GAP
    };
  }

  onMount(() => {
    const query = matchMedia(SHEET_QUERY);
    const onQuery = () => {
      sheetMode = query.matches;
      if (!sheetMode) for (const name of ['--flow-kb', '--flow-vvh', '--flow-sheet-h', '--flow-board-left']) editorEl?.style.removeProperty(name);
      syncViewport();
    };
    onQuery();
    if (sheetMode) canvas?.fitView();
    query.addEventListener('change', onQuery);
    visualViewport?.addEventListener('resize', syncViewport);
    visualViewport?.addEventListener('scroll', syncViewport);
    return () => {
      query.removeEventListener('change', onQuery);
      visualViewport?.removeEventListener('resize', syncViewport);
      visualViewport?.removeEventListener('scroll', syncViewport);
    };
  });

  onMount(() => {
    const preload = () => {
      void loadInspector();
      void loadLibrary();
      void loadTestRun();
    };
    if ('requestIdleCallback' in window) {
      const id = requestIdleCallback(preload);
      return () => cancelIdleCallback(id);
    }
    const id = setTimeout(preload, 200);
    return () => clearTimeout(id);
  });

  const active = () => (document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null);

  function toggleLibrary(mode: LibraryMode, byKey = false) {
    libReturn = active();
    libMode = libMode === mode ? null : mode;
    if (libMode && byKey) libFocus++;
  }

  function closeLibrary(restore = false) {
    libMode = null;
    const back = restore ? libReturn : null;
    libReturn = null;
    if (back) void tick().then(() => back.isConnected && back.focus({ preventScroll: true }));
  }

  function openSettings(id: string) {
    selectedId = id;
    inspecting = true;
    inspectFocus++;
  }

  function closeInspector() {
    inspecting = false;
    if (selectedId) canvas?.focusNode(selectedId);
  }

  function addStep(other: boolean): boolean {
    if (selectedId) return canvas?.openAddMenu(other) ?? false;
    libReturn = active();
    libMode = 'trigger';
    libFocus++;
    return true;
  }

  function settle(kind: 'edit' | 'move' | null, message: string): boolean {
    if (!kind) return false;
    if (selectedId && !flow.nodes.some((n) => n.id === selectedId)) selectedId = null;
    canvas?.settle(kind === 'edit');
    canvas?.announce(message);
    return true;
  }

  function escape(): boolean {
    if (canvas?.dismissMenu()) return true;
    if (libMode) closeLibrary(true);
    else if (inspecting && selectedId) closeInspector();
    else if (testRunOpen) closeTestRun(true);
    else if (selectedId) selectedId = null;
    else return false;
    return true;
  }

  function run(s: Shortcut, e: KeyboardEvent): boolean {
    switch (s.id) {
      case 'escape':
        return escape();
      case 'delete':
        return canvas?.removeSelected() ?? false;
      case 'walk':
        return s.dir ? (canvas?.walk(s.dir) ?? false) : false;
      case 'nudge':
        return s.dir ? (canvas?.nudge(s.dir) ?? false) : false;
      case 'open': {
        const id = (e.target as HTMLElement).closest<HTMLElement>('[data-node-id]')?.dataset.nodeId;
        if (id) openSettings(id);
        return Boolean(id);
      }
      case 'undo':
        return settle(edits.undo(flow), t('flows.noteUndo'));
      case 'redo':
        return settle(edits.redo(flow), t('flows.noteRedo'));
      case 'duplicate':
        return canvas?.duplicateSelected() ?? false;
      case 'save':
        if (dirty) save();
        return true;
      case 'zoom-in':
      case 'zoom-out':
        canvas?.zoomBy(s.id === 'zoom-in' ? 1.2 : 1 / 1.2);
        return true;
      case 'fit':
        canvas?.fitView(panelRoom);
        return true;
      case 'add':
      case 'add-other':
        return addStep(s.id === 'add-other');
    }
  }

  function onKeyDown(e: KeyboardEvent) {
    const s = resolveShortcut(e);
    if (!s || e.isComposing) return;
    const target = e.target as HTMLElement | null;
    if (s.id === 'save') e.preventDefault();
    if (!allowed(s, target) || (e.repeat && !s.repeat)) return;
    if (run(s, e)) e.preventDefault();
  }

  function onAdd(type: string) {
    const sel = selectedId ? flow.nodes.find((n) => n.id === selectedId) : undefined;
    const selSpec = sel ? specFor(sel.type) : undefined;
    const node = addNode(
      flow,
      type,
      sel ? { x: sel.x + 360, y: sel.y } : { x: 0, y: 0 },
      sel && selSpec ? { from: sel.id, port: outputsFor(selSpec)[0].id } : undefined
    );
    selectedId = node.id;
    inspecting = true;
    const fromList = Boolean(flyoutEl?.contains(document.activeElement));
    if (sheetMode || specFor(type)?.kind === 'trigger') {
      libMode = null;
      if (fromList) canvas?.focusNode(node.id);
    }
    if (autoTidy) queueMicrotask(() => canvas?.tidyNow());
  }

  function onBoardPointerDown(e: PointerEvent) {
    if (libMode && flyoutEl && !flyoutEl.contains(e.target as Node)) libMode = null;
  }

  const enabled = $derived(savedEnabled.get(flow.id) ?? flow.enabled);

  function save() {
    baseline = contentOf(flow);
    savedPreviews.set(flow.id, flowPreview(flow));
    toast('success', `Saved changes to "${flow.name}".`);
  }

  async function testRun() {
    runReturn = active();
    selectedId = null;
    testRunOpen = !testRunOpen;
    await tick();
    canvas?.fitView(testRunOpen ? PANEL_ROOM : 0);
  }

  function closeTestRun(restore = false) {
    testRunOpen = false;
    canvas?.fitView();
    const back = restore ? runReturn : null;
    if (back) void tick().then(() => back.isConnected && back.focus({ preventScroll: true }));
  }

  $effect(() => {
    if (!selectedId) inspecting = false;
  });

  $effect(() => {
    if (!inspecting) inspectFocus = 0;
  });

  $effect(() => {
    if (inspecting && selectedId) testRunOpen = false;
  });
</script>

<svelte:head><title>{flow.name} · {data.navLabels['nav.flows']} · ItsBagelBot</title></svelte:head>

<svelte:window onkeydown={onKeyDown} />

<div class="editor" data-canvas="fill" data-flow-editor bind:this={editorEl}>
  <header class="bar" bind:this={barEl}>
    <div class="bar-start">
      <a class="back" href="/flows">
        <Icon name="arrowLeft" size={16} />
        <span class="back-text">{data.navLabels['nav.flows']}</span>
      </a>
      <span class="flow-name">{flow.name}</span>
      <Switch
        checked={enabled}
        label={enabled ? `Turn off ${flow.name}` : `Turn on ${flow.name}`}
        onCheckedChange={(next) => savedEnabled.set(flow.id, next)}
      />
    </div>

    <div class="bar-end">
      <Button variant="ghost" onclick={testRun}><Icon name="play" size={14} /> <span class="run-text">Test run</span></Button>
      <Button variant="primary" disabled={!dirty} onclick={save}>Save</Button>
    </div>
  </header>

  <div class="body">
    <nav class="rail" aria-label="Step tools" bind:this={railEl}>
      {#each TOOLS as tool (tool.mode)}
        <button
          class="tool"
          type="button"
          aria-pressed={libMode === tool.mode}
          onpointerenter={loadLibrary}
          onclick={(e) => toggleLibrary(tool.mode, e.detail === 0)}
        >
          <Icon name={tool.icon} size={22} />
          <span>{tool.label}</span>
        </button>
      {/each}
      <Tooltip text="Search all steps" placement="bottom">
        <button class="tool" type="button" aria-label="Search all steps" aria-pressed={libMode === 'search'} onclick={(e) => toggleLibrary('search', e.detail === 0)}>
          <Icon name="search" size={22} />
        </button>
      </Tooltip>
    </nav>

    <div class="board" role="presentation" bind:this={boardEl} onpointerdowncapture={onBoardPointerDown}>
      <FlowCanvas
        bind:this={canvas}
        bind:flow
        bind:selectedId
        bind:autoTidy
        {trace}
        insets={viewInsets}
        room={panelRoom}
        onmoved={(moves) => edits.moved(moves)}
        oninspect={() => (inspecting = true)}
        onstart={(byKey) => toggleLibrary('trigger', byKey)}
      />

      {#if libMode}
        <div class="flyout" bind:this={flyoutEl} use:sheetDrag={() => (libMode = null)}>
          <span class="grip" data-sheet-grip aria-hidden="true"></span>
          {#await loadLibrary() then { default: StepLibrary }}
            {#key libMode}
              <StepLibrary mode={libMode} focusToken={libFocus} {onAdd} onClose={() => closeLibrary(true)} />
            {/key}
          {/await}
        </div>
      {/if}

      {#if selectedId && inspecting}
        <div class="inspector" use:sheetDrag={() => (selectedId = null)}>
          <span class="grip" data-sheet-grip aria-hidden="true"></span>
          {#await loadInspector() then { default: NodeInspector }}
            <NodeInspector bind:flow bind:selectedId focusToken={inspectFocus} onclose={closeInspector} />
          {/await}
        </div>
      {:else if testRunOpen}
        <div class="inspector" use:sheetDrag={closeTestRun}>
          <span class="grip" data-sheet-grip aria-hidden="true"></span>
          {#await loadTestRun() then { default: TestRunPanel }}
            <TestRunPanel {flow} onClose={closeTestRun} onTrace={(next) => (trace = next)} />
          {/await}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .editor {
    --editor-top: calc(var(--bb-topbar-height) + 6px);
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    margin-top: calc(-1 * var(--editor-top));
    height: 100dvh;
    overflow: hidden;
    background: var(--bb-black);
  }
  @media (max-width: 760px) {
    .editor {
      --editor-top: calc(var(--bb-topbar-height) + 10px);
    }
  }
  .bar {
    position: absolute;
    top: calc(var(--editor-top) + 16px);
    left: 16px;
    right: 16px;
    z-index: 6;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    pointer-events: none;
  }
  .bar-start,
  .bar-end {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 48px;
    min-width: 0;
    padding: 6px;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
    pointer-events: auto;
  }
  .bar-start {
    gap: 12px;
    padding-inline-end: 14px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex: none;
    height: 36px;
    padding: 0 10px;
    border-radius: 8px;
    color: rgba(var(--bb-white-rgb), 0.92);
    font-family: var(--bb-font-body);
    font-size: 14px;
    text-decoration: none;
    transition: background var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .back:hover,
  .back:focus-visible {
    background: rgba(var(--bb-white-rgb), 0.08);
  }
  .flow-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--bb-font-body);
    font-size: 15px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .body {
    position: relative;
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .rail {
    position: absolute;
    top: calc(var(--editor-top) + 80px);
    left: 16px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 72px;
    padding: 6px;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
  }
  .tool {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    min-height: 60px;
    padding: 8px 0;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: rgba(var(--bb-white-rgb), 0.92);
    font-family: var(--bb-font-body);
    font-size: 14px;
    cursor: pointer;
    transition:
      background 0.15s ease,
      color 0.15s ease;
  }
  .tool:hover {
    background: rgba(var(--bb-white-rgb), 0.06);
    color: var(--bb-white);
  }
  .tool[aria-pressed='true'] {
    background: rgba(var(--bb-white-rgb), 0.1);
    color: var(--bb-white);
  }
  .tool:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: -2px;
  }
  .board {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: hidden;
    background: var(--bb-black);
  }
  .flyout {
    position: absolute;
    top: calc(var(--editor-top) + 80px);
    bottom: 16px;
    left: 100px;
    z-index: 5;
    width: 320px;
    min-height: 0;
    overflow: hidden;
    border-radius: 12px;
    background: var(--bb-bg-1);
    box-shadow: 0 16px 48px rgba(var(--bb-shadow-rgb), 0.65);
  }
  .inspector {
    position: absolute;
    top: calc(var(--editor-top) + 80px);
    right: 16px;
    bottom: 16px;
    z-index: 4;
    width: 360px;
    min-height: 0;
  }
  .grip {
    display: none;
  }
  :global(.bb-shell:has(.editor[data-canvas='fill'])) {
    touch-action: pan-x pan-y;
  }
  @media (pointer: coarse), (max-width: 760px) {
    :global(.bb-shell:has(.editor[data-canvas='fill'])) {
      height: 100dvh;
      min-height: 0;
      overflow: hidden;
    }
  }
  @media (pointer: coarse) {
    .bar,
    .rail {
      touch-action: none;
    }
    .back {
      height: 44px;
    }
    .bar-end :global(.bb-btn) {
      min-height: 44px;
    }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    :global(body:has(.editor[data-canvas='fill']) .bb-popover--bottom) {
      display: none;
    }
    .editor {
      --flow-dock-room: calc(81px + 8px + env(safe-area-inset-bottom, 0px));
    }
    .bar {
      left: max(16px, env(safe-area-inset-left, 0px));
      right: max(16px, env(safe-area-inset-right, 0px));
      gap: 8px;
    }
    .bar-start {
      flex: 1;
      gap: 4px;
      padding: 4px 10px 4px 4px;
    }
    .bar-end {
      flex: none;
      gap: 4px;
      min-height: 52px;
      padding: 4px;
    }
    .back {
      justify-content: center;
      width: 44px;
      height: 44px;
      padding: 0;
    }
    .back-text,
    .run-text {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
      white-space: nowrap;
    }
    .flow-name {
      flex: 1;
    }
    .bar-end :global(.bb-btn) {
      min-width: 44px;
      min-height: 44px;
      --btn-pad: 0 12px;
    }
    .rail {
      top: auto;
      right: calc(max(16px, env(safe-area-inset-right, 0px)) + 72px);
      bottom: calc(var(--flow-dock-room) + 8px);
      left: max(16px, env(safe-area-inset-left, 0px));
      flex-direction: row;
      width: auto;
      height: 68px;
      box-sizing: border-box;
      padding: 4px;
    }
    .rail > :global(*) {
      flex: 1;
      min-width: 0;
    }
    .tool {
      gap: 4px;
      height: 100%;
      min-height: 44px;
      padding: 4px 0;
    }
  }
  @media (min-width: 761px) and (pointer: coarse) and (max-height: 520px) {
    .editor {
      --flow-dock-room: calc(8px + env(safe-area-inset-bottom, 0px));
    }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-width: 1023px) {
    .flyout,
    .inspector {
      position: fixed;
      top: auto;
      right: 0;
      bottom: var(--flow-kb, 0px);
      left: var(--flow-board-left, 0px);
      z-index: 70;
      width: auto;
      height: min(var(--flow-sheet-h, 56dvh), calc(var(--flow-vvh, 100dvh) - 64px));
      border-radius: var(--bb-radius-md) var(--bb-radius-md) 0 0;
      background: var(--bb-bg-1);
      box-shadow: 0 -16px 48px rgba(var(--bb-shadow-rgb), 0.65);
      touch-action: pan-y;
      overflow: hidden;
      animation: flow-sheet-in var(--bb-dur-base) var(--bb-ease-out-expo);
    }
    .flyout {
      height: min(calc(var(--flow-sheet-h, 56dvh) * 1.25), calc(var(--flow-vvh, 100dvh) - 64px));
    }
    .grip {
      position: absolute;
      top: 0;
      left: 72px;
      right: 72px;
      z-index: 1;
      display: flex;
      justify-content: center;
      height: 20px;
      padding-top: 6px;
      box-sizing: border-box;
      touch-action: none;
    }
    .grip::after {
      content: '';
      width: 36px;
      height: 4px;
      border-radius: var(--bb-radius-pill);
      background: rgba(var(--bb-white-rgb), 0.24);
    }
    .flyout :global(header),
    .inspector :global(header) {
      padding-top: 24px;
      touch-action: none;
    }
    .inspector :global(aside) {
      border-radius: inherit;
      box-shadow: none;
    }
    .inspector :global(footer) {
      padding-bottom: calc(12px + env(safe-area-inset-bottom, 0px));
    }
    .flyout :global(header .bb-btn--icon.bb-btn--sm),
    .inspector :global(header .bb-btn--icon.bb-btn--sm) {
      min-width: 44px;
      width: 44px;
      height: 44px;
    }
    .editor .flyout :global(:is(input, textarea, select)),
    .editor .inspector :global(:is(input, textarea, select)) {
      font-size: 16px;
    }
    .inspector :global(.scroll),
    .flyout :global(.bb-scroller) {
      overscroll-behavior: contain;
    }
  }
  @keyframes flow-sheet-in {
    from {
      transform: translateY(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .flyout,
    .inspector {
      animation: none;
    }
  }
</style>
