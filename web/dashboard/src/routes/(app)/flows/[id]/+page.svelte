<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick, untrack } from 'svelte';
  import { beforeNavigate, goto } from '$app/navigation';
  import { page } from '$app/state';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ButtonLink from '@bagel/ui/svelte/ButtonLink.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import EditorStage from '@bagel/ui/svelte/EditorStage.svelte';
  import EmptyState from '@bagel/ui/svelte/EmptyState.svelte';
  import Icon from '#lib/components/flow/FlowIcon.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import Tooltip from '@bagel/ui/svelte/Tooltip.svelte';
  import { createDiscardGuard } from '@bagel/ui/svelte/discard-guard';
  import { dismissToast, toast } from '@bagel/ui/svelte/toast';
  import { getI18n } from '@bagel/kit';
  import FlowCanvas, { type FlowCanvasApi, type FlowTrace } from '#lib/components/flow/FlowCanvas.svelte';
  import type { LibraryMode } from '#lib/components/flow/StepLibrary.svelte';
  import { postAction } from '#lib/client-actions.svelte';
  import { FlowHistory } from '#lib/flow/history';
  import { allowed, resolveShortcut, type Scopes, type Shortcut } from '#lib/flow/keyboard';
  import { isStale, saveMessage, stepOf, toggleMessage, upgradeNotes, warningMessages } from '#lib/flow/messages';
  import { awaitingSetup } from '#lib/flow/setup';
  import { addStep as insertStep, selectedAddPoint, selectedPlacement, selectionAfterHistory } from '#lib/flow/editor';
  import { keyboardSize, panelInsets } from '#lib/flow/editor-viewport';
  import { newFlow, specFor, type Flow } from '#lib/flow/model';
  import { rehearseFlow } from '#lib/flow/rehearse';
  import { scenarioPatches } from '@bagel/kit/automations';
  import type { RehearsalScenario } from '#lib/flow/rehearse';
  import { flowRouteId, parseFlowRouteId } from '#lib/flow/ref';
  import type { SaveReply, ToggleReply } from '#lib/flow/replies';
  import type { UpgradeReport } from '#lib/server/flow-store';
  import { SHEET_QUERY, sheetDrag } from '#lib/flow/sheet';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  const TOOLS = [
    { mode: 'trigger', icon: 'bolt' },
    { mode: 'condition', icon: 'branch' },
    { mode: 'reply', icon: 'messageSquare' }
  ] as const;

  const toolLabel = (mode: (typeof TOOLS)[number]['mode']) => {
    if (mode === 'trigger') return t('flows.board.when');
    return mode === 'condition' ? t('flows.board.onlyIf') : t('flows.board.do');
  };

  const PANEL_ROOM = 392;
  const SHEET_SHARE = 0.56;
  const TALL_SHEET_SHARE = 0.44;
  const TALL_VIEWPORT = 900;

  const loadInspector = () => import('#lib/components/flow/NodeInspector.svelte');
  const loadLibrary = () => import('#lib/components/flow/StepLibrary.svelte');
  const loadTestRun = () => import('#lib/components/flow/TestRunPanel.svelte');
  const loadRecipe = () => import('#lib/components/flow/RecipePanel.svelte');

  const { t } = getI18n();

  const opened = untrack(() => structuredClone(data.flow ?? newFlow('')));
  let flow = $state<Flow>({ ...opened, name: opened.name || t('flows.page.untitled') });
  const contentOf = (f: Flow) =>
    JSON.stringify({ name: f.name, queue: f.queue ?? '', edges: f.edges, nodes: f.nodes.map((n) => ({ id: n.id, type: n.type, config: n.config })) });

  let baseline = $state(contentOf(untrack(() => flow)));
  const drawable = untrack(() => data.flow !== null);
  let ref = $state(untrack(() => data.ref));
  let revision = $state(untrack(() => data.revision));
  let enabled = $state(untrack(() => flow.enabled));
  let saving = $state(false);
  let staleLatest = $state<string | null>(null);
  let upgrade = $state<UpgradeReport | null>(null);
  let leaving = false;
  let selectedId = $state<string | null>(null);
  let testRunOpen = $state(false);
  let recipeOpen = $state(false);
  let scenario = $state<Partial<RehearsalScenario>>({});
  let overrides = $state<Record<string, string>>({});
  const rehearsal = $derived(rehearseFlow(flow, t, { scenario, overrides }));
  const trace = $derived<FlowTrace | null>(testRunOpen ? {
    nodes: new Set(rehearsal.steps.filter((step) => step.kind !== 'skip' && step.status !== 'skipped').flatMap((step) => step.id ? [step.id] : [])),
    edges: new Set(flow.edges.filter((edge) => tracedEdge(edge)).map((edge) => edge.id)),
    current: selectedId
  } : null);
  let autoTidy = $state(true);
  let canvas = $state<FlowCanvasApi>();
  let libMode = $state<LibraryMode | null>(untrack(() => (data.flow?.nodes.length === 0 ? 'trigger' : null)));
  let editorEl = $state<HTMLDivElement>();
  let barEl = $state<HTMLElement>();
  let railEl = $state<HTMLElement>();
  let boardEl = $state<HTMLDivElement>();
  let leftPanelEl = $state<HTMLDivElement>();
  let rightPanelEl = $state<HTMLDivElement>();
  let sheetMode = $state(false);
  let wideBoard = $state(false);
  let inspecting = $state(false);
  let inspectFocus = $state(0);
  let libFocus = $state(0);
  let libReturn: HTMLElement | null = null;
  let runReturn: HTMLElement | null = null;
  let recipeReturn: HTMLElement | null = null;

  const edits = new FlowHistory(untrack(() => flow));
  let canUndo = $state(false);
  let canRedo = $state(false);
  let deletionToast: { id: number; entry: object | null } | null = null;
  const dirty = $derived(contentOf(flow) !== baseline);
  const inspectorVisible = $derived(Boolean(selectedId && inspecting && (!testRunOpen || wideBoard)));
  const panelRoom = $derived.by(() => {
    if (testRunOpen && inspectorVisible) return PANEL_ROOM * 2;
    return inspectorVisible || testRunOpen || recipeOpen ? PANEL_ROOM : 0;
  });

  function tracedEdge(edge: Flow['edges'][number]): boolean {
    const source = rehearsal.steps.filter((step) => step.id === edge.from);
    const reached = rehearsal.steps.some((step) => step.id === edge.to);
    return reached && source.some((step) => !step.port || step.port === edge.port);
  }

  function changeScenario(patch: Partial<RehearsalScenario>) {
    scenario = { ...scenario, ...patch };
  }

  $effect(() => {
    const trigger = flow.nodes.find((node) => specFor(node.type)?.kind === 'trigger');
    const kind = `${trigger?.type}:${trigger?.config.variant ?? ''}`;
    if (kind) untrack(() => { scenario = scenarioPatches(rehearsal.fields, scenario); });
  });

  $effect(() => {
    edits.sync(flow);
    refreshHistory();
  });

  function refreshHistory() {
    canUndo = edits.canUndo;
    canRedo = edits.canRedo;
    if (deletionToast && deletionToast.entry !== edits.undoEntry) {
      dismissToast(deletionToast.id);
      deletionToast = null;
    }
  }

  function history(way: 'undo' | 'redo'): boolean {
    edits.sync(flow);
    const before = new Set(flow.nodes.map((node) => node.id));
    const parent = flow.edges.find((edge) => edge.to === selectedId)?.from ?? null;
    const fallback = selectedId;
    const kind = edits[way](flow);
    selectedId = selectionAfterHistory(flow, before, fallback) ?? selectionAfterHistory(flow, before, parent);
    refreshHistory();
    const changed = settle(kind, way === 'undo' ? t('flows.noteUndo') : t('flows.noteRedo'));
    if (changed && selectedId) canvas?.focusNode(selectedId);
    return changed;
  }

  function removed(count: number) {
    inspecting = false;
    edits.sync(flow);
    refreshHistory();
    if (deletionToast) dismissToast(deletionToast.id);
    const entry = edits.undoEntry;
    const message = count === 1 ? t('flows.board.stepRemoved') : t('flows.board.stepsRemoved', { n: count });
    const id = toast('success', message, { undoLabel: t('flows.board.undo'), onUndo: () => undoRemoval(entry) });
    deletionToast = { id, entry };
  }

  function undoRemoval(entry: object | null) {
    edits.sync(flow);
    refreshHistory();
    if (edits.undoEntry === entry) history('undo');
  }

  const layoutHeight = () => document.documentElement.clientHeight;
  const sheetHeight = () => Math.round(layoutHeight() * (layoutHeight() >= TALL_VIEWPORT ? TALL_SHEET_SHARE : SHEET_SHARE));

  function syncViewport() {
    const style = editorEl?.style;
    if (!style || !sheetMode) return;
    const { height, keyboard } = keyboardSize(layoutHeight(), visualViewport);
    style.setProperty('--stage-kb', `${keyboard}px`);
    style.setProperty('--stage-vvh', `${height}px`);
    style.setProperty('--stage-sheet-h', `${sheetHeight()}px`);
    style.setProperty('--stage-board-left', `${boardEl?.getBoundingClientRect().left ?? 0}px`);
    if (keyboard === 0 && scrollY !== 0) scrollTo(0, 0);
    revealKeyboardField(keyboard);
  }

  function revealKeyboardField(keyboard: number) {
    if (keyboard <= 0) return;
    const field = document.activeElement;
    if (!field?.matches('input, textarea, select')) return;
    if (field.closest('.bb-editor-stage__panel')) field.scrollIntoView({ block: 'center' });
  }

  function viewInsets(withPanel: boolean) {
    if (!sheetMode || !boardEl) return null;
    if (!barEl || !railEl) return null;
    return panelInsets({ board: boardEl.getBoundingClientRect(), bar: barEl.getBoundingClientRect(), rail: railEl.getBoundingClientRect() }, withPanel ? sheetHeight() : 0);
  }

  onMount(() => {
    const query = matchMedia(SHEET_QUERY);
    const wide = matchMedia('(min-width: 1280px)');
    const onWidth = () => { wideBoard = wide.matches; };
    onWidth();
    wide.addEventListener('change', onWidth);
    const onQuery = () => {
      sheetMode = query.matches;
      if (!sheetMode) for (const name of ['--stage-kb', '--stage-vvh', '--stage-sheet-h', '--stage-board-left']) editorEl?.style.removeProperty(name);
      syncViewport();
    };
    onQuery();
    if (sheetMode) canvas?.fitView();
    query.addEventListener('change', onQuery);
    visualViewport?.addEventListener('resize', syncViewport);
    visualViewport?.addEventListener('scroll', syncViewport);
    return () => {
      wide.removeEventListener('change', onWidth);
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
      void loadRecipe();
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
    if (!testRunOpen) inspectFocus++;
    void tick().then(() => canvas?.revealNode(id));
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
    if (dismissPanel()) return true;
    if (!selectedId) return false;
    selectedId = null;
    return true;
  }

  function dismissPanel(): boolean {
    if (libMode) closeLibrary(true);
    else if (inspectorVisible) closeInspector();
    else if (testRunOpen) closeTestRun(true);
    else if (recipeOpen) closeRecipe(true);
    else return false;
    return true;
  }

  function run(s: Shortcut, e: KeyboardEvent): boolean {
    const actions = {
      escape,
      delete: () => canvas?.removeSelected() ?? false,
      walk: () => moveSelection(s, 'walk'),
      nudge: () => moveSelection(s, 'nudge'),
      open: () => inspectTarget(e.target),
      undo: () => history('undo'),
      redo: () => history('redo'),
      duplicate: () => canvas?.duplicateSelected() ?? false,
      save: saveShortcut,
      'zoom-in': () => zoomShortcut(1.2),
      'zoom-out': () => zoomShortcut(1 / 1.2),
      fit: fitShortcut,
      add: () => addStep(false),
      'add-other': () => addStep(true)
    };
    return actions[s.id]();
  }

  function moveSelection(shortcut: Shortcut, action: 'walk' | 'nudge'): boolean {
    return shortcut.dir ? canvas?.[action](shortcut.dir) ?? false : false;
  }

  function inspectTarget(target: EventTarget | null): boolean {
    const id = canvas?.nodeIdAt(target);
    if (id) openSettings(id);
    return Boolean(id);
  }

  function saveShortcut(): boolean {
    if (dirty) void save();
    return true;
  }

  function zoomShortcut(factor: number): boolean {
    canvas?.zoomBy(factor);
    return true;
  }

  function fitShortcut(): boolean {
    canvas?.fitView(panelRoom);
    return true;
  }

  const scopes: Scopes = {
    canvas: (target) => canvas?.hasTarget(target) ?? false,
    editor: (target) => editorEl?.contains(target as Node | null) ?? false,
    node: (target) => canvas?.nodeIdAt(target) !== undefined
  };

  function onKeyDown(e: KeyboardEvent) {
    const s = resolveShortcut(e);
    if (!s || e.isComposing) return;
    const target = e.target as HTMLElement | null;
    if (s.id === 'save') e.preventDefault();
    if (!allowed(s, target, scopes)) return;
    if (e.repeat && !s.repeat) return;
    if (run(s, e)) e.preventDefault();
  }

  function onAdd(type: string) {
    const { node, replaced } = insertStep(flow, {
      type,
      at: selectedAddPoint(flow, selectedId),
      placement: selectedPlacement(flow, selectedId),
      chatMessage: t('flows.board.defaultChatMessage'),
      plainMessage: t('flows.board.defaultPlainMessage')
    });
    if (sheetMode || specFor(type)?.kind === 'trigger') libMode = null;
    openSettings(node.id);
    if (replaced) canvas?.announce(t('flows.board.triggerReplaced'));
    if (autoTidy) queueMicrotask(() => canvas?.tidyNow());
  }

  function onBoardPointerDown(e: PointerEvent) {
    if (!libMode || !leftPanelEl) return;
    if (!leftPanelEl.contains(e.target as Node)) libMode = null;
  }

  function rightPanelDismiss(): void {
    if (inspectorVisible) selectedId = null;
    else if (testRunOpen) closeTestRun();
    else if (recipeOpen) closeRecipe();
  }

  $effect(() => {
    if (!leftPanelEl) return;
    const drag = sheetDrag(leftPanelEl, () => (libMode = null));
    return () => drag.destroy();
  });

  $effect(() => {
    if (!rightPanelEl) return;
    const drag = sheetDrag(rightPanelEl, rightPanelDismiss);
    return () => drag.destroy();
  });

  const discard = createDiscardGuard(() => dirty && !leaving);

  beforeNavigate(({ cancel, to, type }) => {
    if (!dirty || leaving) return;
    cancel();
    if (type === 'leave' || !to) return;
    const target = to.url;
    discard.guard(() => {
      leaving = true;
      void goto(target);
    });
  });

  function focusStep(id: string) {
    selectedId = id;
    inspecting = true;
    inspectFocus++;
    canvas?.focusNode(id);
  }

  function announceWarnings(warnings: Extract<SaveReply, { ok: true }>['warnings']) {
    for (const text of warningMessages(t, warnings)) toast('info', text, { ttlMs: 8000 });
  }

  function adoptRewardId(drawn: Flow, rewardId: string | undefined) {
    if (!rewardId) return;
    for (const node of drawn.nodes) if (node.type === 'twitch.reward') node.config.reward = rewardId;
  }

  function saved(reply: Extract<SaveReply, { ok: true }>, sent: Flow) {
    ref = parseFlowRouteId(reply.id);
    revision = reply.revision;
    enabled = reply.enabled;
    flow.name = reply.name;
    flow.enabled = reply.enabled;
    for (const drawn of [flow, sent]) adoptRewardId(drawn, reply.rewardId);
    baseline = contentOf({ ...sent, name: reply.name });
    toast('success', t('flows.saved', { name: reply.name }));
    announceWarnings(reply.warnings);
    if (reply.id !== page.params.id) void goto(`/flows/${reply.id}`, { replaceState: true });
  }

  function refused(reply: Extract<SaveReply, { ok: false }>) {
    if (reply.reason === 'needs_upgrade' && reply.upgrade) {
      upgrade = reply.upgrade;
    } else if (isStale(reply) && reply.latestRevision) {
      staleLatest = reply.latestRevision;
    } else {
      const step = stepOf(reply.issues ?? [], flow.nodes);
      const message = saveMessage(t, reply);
      toast('danger', step ? `${message} ${t('flows.saveStep')}` : message, { ttlMs: 9000 });
      if (step) focusStep(step);
    }
  }

  async function save(extra: { revision?: string; confirmUpgrade?: boolean } = {}) {
    if (saving) return;
    saving = true;
    const sent = $state.snapshot(flow) as Flow;
    const body = new FormData();
    body.set('payload', JSON.stringify({ flow: sent, ref, expectedRevision: extra.revision ?? revision ?? undefined, confirmUpgrade: extra.confirmUpgrade }));
    const reply = await postAction<SaveReply>('save', body);
    saving = false;
    if (!reply) toast('danger', t('flows.saveFailed'));
    else if (reply.ok) saved(reply, sent);
    else refused(reply);
  }

  async function refuseUntilSetUp(step: string) {
    enabled = true;
    await tick();
    enabled = false;
    toast('danger', t('flows.setup.refused'), { ttlMs: 9000 });
    focusStep(step);
  }

  async function setEnabled(next: boolean) {
    const unfinished = next ? flow.nodes.find(awaitingSetup) : undefined;
    if (unfinished) return refuseUntilSetUp(unfinished.id);
    const before = enabled;
    enabled = next;
    flow.enabled = next;
    if (!ref) return;
    const body = new FormData();
    body.set('id', flowRouteId(ref));
    body.set('enabled', next ? '1' : '0');
    body.set('revision', revision ?? '');
    const reply = await postAction<ToggleReply>('toggle', body, '/flows');
    if (reply?.ok) {
      revision = reply.revision;
      announceWarnings(reply.warnings);
      return;
    }
    enabled = before;
    flow.enabled = before;
    toast('danger', reply ? toggleMessage(t, reply) : t('flows.toggleFailed'));
  }

  async function testRun() {
    runReturn = active();
    selectedId = null;
    recipeOpen = false;
    testRunOpen = !testRunOpen;
    await tick();
    canvas?.fitView(testRunOpen ? PANEL_ROOM : 0);
  }

  async function toggleRecipe() {
    recipeReturn = active();
    selectedId = null;
    testRunOpen = false;
    recipeOpen = !recipeOpen;
    await tick();
    canvas?.fitView(recipeOpen ? PANEL_ROOM : 0);
  }

  function closeRecipe(restore = false) {
    recipeOpen = false;
    canvas?.fitView();
    const back = restore ? recipeReturn : null;
    if (back) void tick().then(() => back.isConnected && back.focus({ preventScroll: true }));
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
    if (inspecting && selectedId) {
      recipeOpen = false;
    }
  });
</script>

<svelte:head><title>{flow.name} · {data.navLabels['nav.flows']} · ItsBagelBot</title></svelte:head>

<svelte:window onkeydown={onKeyDown} onbeforeunload={(e) => dirty && !leaving && e.preventDefault()} />

{#snippet topStart()}
  <a class="back" href="/flows">
    <Icon name="arrowLeft" size={16} />
    <span class="back-text">{data.navLabels['nav.flows']}</span>
  </a>
  <span class="flow-name"><Input fill maxlength={64} aria-label={t('flows.nameLabel')} bind:value={flow.name} /></span>
  <Switch
    checked={enabled}
    label={enabled ? t('flows.page.turnOff', { name: flow.name }) : t('flows.page.turnOn', { name: flow.name })}
    onCheckedChange={(next) => void setEnabled(next)}
  />
{/snippet}

{#snippet topEnd()}
  <Button variant="ghost" onclick={testRun}><Icon name="play" size={14} /> <span class="run-text">{t('flows.board.testRun')}</span></Button>
  <Button variant="primary" disabled={!dirty} busy={saving} onclick={() => void save()}>{t('flows.page.save')}</Button>
{/snippet}

{#snippet rail()}
  {#each TOOLS as tool (tool.mode)}
    <button
      class="tool"
      type="button"
      aria-pressed={libMode === tool.mode}
      onpointerenter={loadLibrary}
      onclick={(e) => toggleLibrary(tool.mode, e.detail === 0)}
    >
      <Icon name={tool.icon} size={22} />
      <span>{toolLabel(tool.mode)}</span>
    </button>
  {/each}
  <Tooltip text={t('flows.board.searchAll')} placement="bottom">
    <button class="tool" type="button" aria-label={t('flows.board.searchAll')} aria-pressed={libMode === 'search'} onclick={(e) => toggleLibrary('search', e.detail === 0)}>
      <Icon name="search" size={22} />
    </button>
  </Tooltip>
  <Tooltip text={t('flows.recipe.title')} placement="bottom">
    <button class="tool" type="button" aria-label={t('flows.recipe.title')} aria-pressed={recipeOpen} onpointerenter={loadRecipe} onclick={toggleRecipe}>
      <Icon name="settings" size={22} />
    </button>
  </Tooltip>
{/snippet}

{#snippet leftPanel()}
  {#await loadLibrary() then { default: StepLibrary }}
    {#key libMode}
      <StepLibrary mode={libMode ?? 'trigger'} focusToken={libFocus} {onAdd} onClose={() => closeLibrary(true)} />
    {/key}
  {/await}
{/snippet}

{#snippet rightPanel()}
  <div class="panels">
  {#if inspectorVisible}
    <div class="panel-slot">
    {#await loadInspector() then { default: NodeInspector }}
      <NodeInspector bind:flow bind:selectedId {rehearsal} focusToken={inspectFocus} onclose={closeInspector} onremove={(id) => canvas?.removeStep(id)} />
    {/await}
    </div>
  {/if}
  {#if testRunOpen}
    <div class="panel-slot">
    {#await loadTestRun() then { default: TestRunPanel }}
      <TestRunPanel view={rehearsal} {overrides} onScenario={changeScenario} onOverride={(key, value) => (overrides = { ...overrides, [key]: value })} onSelect={openSettings} onClose={closeTestRun} />
    {/await}
    </div>
  {:else if recipeOpen}
    {#await loadRecipe() then { default: RecipePanel }}
      <RecipePanel bind:flow {ref} onclose={() => closeRecipe(true)} />
    {/await}
  {/if}
  </div>
{/snippet}

{#snippet boardChildren()}
  <FlowCanvas
    bind:this={canvas}
    bind:flow
    bind:selectedId
    bind:autoTidy
    {trace}
    insets={viewInsets}
    room={panelRoom}
    onmoved={(moves) => { edits.moved(moves); refreshHistory(); }}
    oninspect={() => { if (selectedId) openSettings(selectedId); }}
    onadded={openSettings}
    onremoved={removed}
    onundo={() => history('undo')}
    onredo={() => history('redo')}
    {canUndo}
    {canRedo}
    onstart={(byKey) => toggleLibrary('trigger', byKey)}
  />
{/snippet}

{#if drawable}
<EditorStage
  style="--editor-panel-right: {testRunOpen && inspectorVisible ? 'min(720px, calc(100vw - 112px))' : '360px'}"
  height="100dvh"
  topInset="calc(var(--bb-topbar-height) + var(--flows-top-gap, 6px))"
  bottomRoom="var(--flows-bottom-room, 0px)"
  railLabel={t('flows.board.stepTools')}
  bind:editorEl
  bind:barEl
  bind:railEl
  bind:boardEl
  bind:leftPanelEl
  bind:rightPanelEl
  {topStart}
  {topEnd}
  {rail}
  leftPanel={libMode ? leftPanel : undefined}
  rightPanel={inspectorVisible || testRunOpen || recipeOpen ? rightPanel : undefined}
  onboardpointerdown={onBoardPointerDown}
  children={boardChildren}
/>

<ConfirmDialog
  open={discard.open}
  title={t('flows.discardTitle')}
  body={t('flows.discardBody')}
  confirmLabel={t('flows.discardConfirm')}
  cancelLabel={t('flows.discardKeep')}
  tone="danger"
  onCancel={discard.cancel}
  onConfirm={discard.confirm}
/>

<ConfirmDialog
  open={staleLatest !== null}
  title={t('flows.staleTitle')}
  body={t('flows.staleBody')}
  confirmLabel={t('flows.staleOverwrite')}
  cancelLabel={t('flows.staleKeep')}
  onCancel={() => (staleLatest = null)}
  onConfirm={() => {
    const latest = staleLatest ?? undefined;
    staleLatest = null;
    void save({ revision: latest });
  }}
/>

<ConfirmDialog
  open={upgrade !== null}
  title={t('flows.upgradeTitle')}
  body={upgrade?.from === 'alert' ? t('flows.upgradeAlertBody') : t('flows.upgradeCommandBody')}
  confirmLabel={t('flows.upgradeConfirm')}
  onCancel={() => (upgrade = null)}
  onConfirm={() => {
    upgrade = null;
    void save({ confirmUpgrade: true });
  }}
>
  <ul class="notes">
    {#each upgrade ? upgradeNotes(t, upgrade) : [] as note (note)}<li>{note}</li>{/each}
  </ul>
</ConfirmDialog>
{:else}
  <div class="unsupported">
    <EmptyState title={t('flows.unsupportedTitle')} body={t('flows.unsupportedBody')}>
      <ButtonLink variant="primary" href="/automations">{t('flows.unsupportedCta')}</ButtonLink>
    </EmptyState>
  </div>
{/if}

<style>
  .panels { display: flex; height: 100%; min-height: 0; gap: 12px; }
  .panel-slot { flex: 1; min-width: 0; height: 100%; }
  .notes {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0 0 12px;
    padding-left: 18px;
  }
  .unsupported {
    display: flex;
    justify-content: center;
    padding-top: 48px;
  }
  :global(:root) {
    --flows-top-gap: 6px;
  }
  @media (max-width: 760px) {
    :global(:root) {
      --flows-top-gap: 10px;
    }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
    :global(:root) {
      --flows-bottom-room: calc(81px + 8px + env(safe-area-inset-bottom, 0px));
    }
  }
  @media (min-width: 761px) and (pointer: coarse) and (max-height: 520px) {
    :global(:root) {
      --flows-bottom-room: calc(8px + env(safe-area-inset-bottom, 0px));
    }
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
    display: block;
    min-width: 0;
    width: min(280px, 36vw);
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
  @media (pointer: coarse) {
    .back {
      height: 44px;
    }
  }
  @media (max-width: 760px), (pointer: coarse) and (max-height: 520px) {
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
    .tool {
      gap: 4px;
      height: 100%;
      min-height: 44px;
      padding: 4px 0;
    }
  }
</style>
