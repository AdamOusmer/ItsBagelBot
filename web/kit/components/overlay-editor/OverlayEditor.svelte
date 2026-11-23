<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, untrack, type Snippet } from 'svelte';
  import type { Box } from '@bagel/ui/lib/box-transform';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import EditorStage from '@bagel/ui/svelte/EditorStage.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import OverlayCanvas from '@bagel/ui/svelte/OverlayCanvas.svelte';
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import PickerPanel from '@bagel/ui/svelte/PickerPanel.svelte';
  import Tooltip from '@bagel/ui/svelte/Tooltip.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayDocument, OverlayWidget } from '../../lib/overlay-document';
  import type { MediaBinding, SaveOutcome } from '../../lib/overlay-editor/binding';
  import { newWidget, type Canvas, type EditableKind } from '../../lib/overlay-editor/defaults';
  import { assetRef, toWireDocument, widgetLimitReached } from '../../lib/overlay-editor/document';
  import { EmulationBus } from '../../lib/overlay-editor/emulate';
  import type { MediaSlot } from '../../lib/overlay-editor/fields';
  import { acceptsType, describeUpload, MediaFailure, slotMaxBytes, type MediaAdapter } from '../../lib/overlay-editor/media';
  import { addWidget, copyOf, moveWidget, removeWidget, reorderWidget, replaceWidget } from '../../lib/overlay-editor/ops';
  import { resizeCanvas, resolutions } from '../../lib/overlay-editor/resolutions';
  import { EditorSession } from '../../lib/overlay-editor/session';
  import { stepZoom } from '../../lib/overlay-editor/zoom';
  import AddMenu from './AddMenu.svelte';
  import Fit from './Fit.svelte';
  import Notice from './Notice.svelte';
  import RuntimePreview from './RuntimePreview.svelte';
  import SidePanel, { type Section } from './SidePanel.svelte';
  import type { LayerActions } from './LayersList.svelte';
  import Toolbar from './Toolbar.svelte';

  let { initial, media, preview = false, backHref = '/overlays', notice, onsave, ondirty }: {
    initial: OverlayDocument;
    media?: MediaAdapter;
    preview?: boolean;
    backHref?: string;
    notice?: Snippet;
    onsave?: (doc: OverlayDocument) => Promise<SaveOutcome>;
    ondirty?: (dirty: boolean) => void;
  } = $props();

  const { t } = getI18n();
  const session = new EditorSession(untrack(() => initial));
  const bus = new EmulationBus();
  const FIELD_SELECTOR = 'input,textarea,select,[contenteditable],dialog,[role="dialog"],[role="listbox"]';

  let doc = $state.raw(untrack(() => initial));
  let saved = $state.raw(untrack(() => initial));
  let selected = $state('');
  let section = $state<Section>('layers');
  let zoom = $state(0);
  let grid = $state(true);
  let busy = $state(false);
  let failure = $state('');
  let compact = $state(false);
  let panelOpen = $state(false);
  let addOpen = $state(false);
  let addAnchor = $state<HTMLElement>();
  let settingsOpen = $state(false);
  let settingsAnchor = $state<HTMLElement>();
  let shortcut = $state('Ctrl+S');
  let steps = $state({ undo: false, redo: false });
  let scale = 1;

  const dirty = $derived(doc !== saved);
  const widget = $derived(doc.widgets.find((item) => item.id === selected));
  const full = $derived(widgetLimitReached(doc));
  const isCurrent = (size: Canvas): boolean => size.width === doc.width && size.height === doc.height;
  const sizes = $derived<readonly Canvas[]>(resolutions.some(isCurrent) ? resolutions : [...resolutions, { width: doc.width, height: doc.height }]);
  const frameLabel = $derived(sizeLabel(doc));
  const boxes = $derived(doc.widgets.map((item) => ({ ...item, label: item.name })));
  const handleLabels = {
    nw: t('overlayEditor.handle.nw'), ne: t('overlayEditor.handle.ne'),
    sw: t('overlayEditor.handle.sw'), se: t('overlayEditor.handle.se')
  };
  const writable = $derived(!!onsave && !preview);
  const idle = $derived(dirty && !busy);
  const canSave = $derived(idle && writable);
  const binding: MediaBinding | undefined = $derived(
    media && !preview ? { upload, assets: doc.assets } : undefined
  );
  const showPanel = $derived(!compact || panelOpen);

  $effect(() => ondirty?.(dirty));
  onMount(() => {
    if (/mac|iphone|ipad/i.test(navigator.userAgent)) shortcut = 'Cmd+S';
    const query = matchMedia('(max-width: 1279px)');
    const follow = (): void => {
      compact = query.matches;
    };
    follow();
    query.addEventListener('change', follow);
    return () => query.removeEventListener('change', follow);
  });

  function sizeLabel(size: Canvas): string {
    return `${size.width} × ${size.height}`;
  }

  function sync(): void {
    steps = { undo: session.canUndo, redo: session.canRedo };
  }

  function apply(next: OverlayDocument): void {
    doc = next;
    session.commit(next);
    sync();
  }

  function step(direction: -1 | 1): void {
    const next = session.step(direction);
    if (next) doc = next;
    sync();
  }

  function patchWidget(id: string, change: Partial<OverlayWidget>): void {
    const target = doc.widgets.find((item) => item.id === id);
    if (target) apply(replaceWidget(doc, { ...target, ...change }));
  }

  function pickOnCanvas(id: string): void {
    selected = id;
    section = 'settings';
  }

  function addKind(kind: EditableKind): void {
    if (full) return;
    const created = newWidget(kind, doc, t(`overlayEditor.kind.${kind}`));
    apply(addWidget(doc, created));
    addOpen = false;
    pickOnCanvas(created.id);
  }

  function duplicateSelected(): void {
    if (!widget || full) return;
    const copy = copyOf(doc, widget);
    apply(addWidget(doc, copy));
    selected = copy.id;
  }

  function removeSelected(): void {
    if (!widget) return;
    apply(removeWidget(doc, widget.id));
    selected = '';
  }

  const actions: LayerActions = {
    select: (id) => (selected = id),
    patch: patchWidget,
    reorder: (id, offset) => apply(reorderWidget(doc, id, offset)),
    move: (id, to) => apply(moveWidget(doc, id, to)),
    duplicate: duplicateSelected,
    remove: removeSelected
  };

  function moveBox(id: string, box: Box): void {
    const target = doc.widgets.find((item) => item.id === id);
    if (target) doc = replaceWidget(doc, { ...target, ...box });
  }

  function renameOverlay(event: Event & { currentTarget: HTMLInputElement }): void {
    const name = event.currentTarget.value.trim();
    if (name) apply({ ...doc, name });
    else event.currentTarget.value = doc.name;
  }

  function changeSize(size: Canvas): void {
    settingsOpen = false;
    apply(resizeCanvas(doc, size));
  }

  async function upload(file: File, slot: MediaSlot): Promise<string> {
    if (!media) throw new MediaFailure('premium_required');
    if (file.size > slotMaxBytes(slot)) throw new MediaFailure('asset_too_large');
    const uploaded = await media.upload(file);
    if (!acceptsType(slot, uploaded.type)) throw new MediaFailure('unsupported_media');
    apply({ ...doc, assets: { ...doc.assets, [uploaded.asset_id]: describeUpload(uploaded) } });
    return assetRef(uploaded.asset_id);
  }

  async function save(): Promise<void> {
    if (!canSave) return;
    const submitted = doc;
    busy = true;
    failure = '';
    try {
      const outcome = await onsave?.(toWireDocument(submitted));
      if (!outcome?.ok) {
        failure = outcome?.error ?? '';
        return;
      }
      const next = { ...submitted, rev: outcome.revision };
      session.markSaved(next);
      saved = next;
      doc = doc === submitted ? next : { ...doc, rev: outcome.revision };
    } finally {
      busy = false;
    }
  }

  function dismiss(): void {
    if (selected) selected = '';
    else panelOpen = false;
  }

  const commands = new Map<string, (event: KeyboardEvent) => void>([
    ['s', () => void save()],
    ['z', (event) => step(event.shiftKey ? 1 : -1)]
  ]);
  const plainKeys = new Map<string, () => void>([
    ['delete', removeSelected],
    ['backspace', removeSelected],
    ['escape', dismiss]
  ]);

  function keyboard(event: KeyboardEvent): void {
    if (event.isComposing) return;
    const key = event.key.toLowerCase();
    const run = (event.metaKey || event.ctrlKey ? commands : plainKeys).get(key);
    if (!run) return;
    const inField = !!(event.target as HTMLElement | null)?.closest(FIELD_SELECTOR);
    if (inField && key !== 's') return;
    event.preventDefault();
    run(event);
  }
</script>

<svelte:window onkeydown={keyboard} onbeforeunload={(event) => dirty && event.preventDefault()} />

{#snippet canvasPreview(box: { id: string }, boxScale: number)}
  {@const item = doc.widgets.find((candidate) => candidate.id === box.id)}
  {#if item}<RuntimePreview widget={item} assets={doc.assets} scale={boxScale} events={bus} />{/if}
{/snippet}

{#snippet topStart()}
  <Tooltip text={t('overlayEditor.ui.back')} placement="bottom">
    <a class="back" href={backHref} aria-label={t('overlayEditor.ui.back')}><Icon name="arrowLeft" size={18} /></a>
  </Tooltip>
  <span class="overlay-name">
    <Input fill class="name-field" value={doc.name} maxlength={120} aria-label={t('overlayEditor.ui.overlayName')} onchange={renameOverlay} />
  </span>
  <span class="dot" class:on={dirty} role="img" aria-label={t('overlayEditor.ui.unsaved')} aria-hidden={!dirty}></span>
{/snippet}

{#snippet topEnd()}
  <span class="anchor" bind:this={settingsAnchor}>
    <IconButton label={t('overlayEditor.ui.overlaySettings')} tooltip aria-haspopup="dialog" aria-expanded={settingsOpen} onclick={() => (settingsOpen = !settingsOpen)}>
      <Icon name="gear" size={18} />
    </IconButton>
  </span>
  <Tooltip text={t('overlayEditor.ui.saveShortcut', { shortcut })} placement="bottom">
    <Button class="overlay-save" variant={dirty || !writable ? 'primary' : 'ghost'} {busy} disabled={!canSave} onclick={save}>
      <span class="save-check" class:on={!dirty && writable}><Icon name="check" size={14} /></span>
      <Fit>{dirty || !writable ? t('common.save') : t('overlayEditor.ui.saved')}</Fit>
    </Button>
  </Tooltip>
{/snippet}

{#snippet leftPanel()}
  <SidePanel
    {doc}
    {selected}
    {widget}
    {full}
    {binding}
    bind:section
    {actions}
    update={(next) => apply(replaceWidget(doc, next))}
    onadd={() => (addOpen = true)}
    onclose={() => (panelOpen = false)}
  />
{/snippet}

{#snippet board()}
  <div class="canvas-host" role="presentation" onpointerdown={() => (selected = '')}>
    <OverlayCanvas
      class="canvas-fill"
      width={doc.width}
      height={doc.height}
      {boxes}
      {selected}
      {zoom}
      {handleLabels}
      dots={grid}
      {frameLabel}
      tagSelected
      fitMargin={2}
      onscale={(value) => (scale = value)}
      onselect={pickOnCanvas}
      onchange={moveBox}
      oncommit={() => apply(doc)}
      oncancel={() => (doc = session.revert())}
      preview={canvasPreview}
    />
  </div>
  <div class="add-slot" bind:this={addAnchor}>
    <Button
      class="fab"
      aria-label={addOpen ? t('overlayEditor.ui.closeMenu') : t('overlayEditor.ui.addWidget')}
      aria-haspopup="dialog"
      aria-expanded={addOpen}
      onclick={() => (addOpen = !addOpen)}
    >
      <Icon name={addOpen ? 'x' : 'plus'} size={22} />
    </Button>
  </div>
  <div class="dock">
    <Toolbar
      canUndo={steps.undo}
      canRedo={steps.redo}
      bind:grid
      panelOpen={showPanel}
      onemulate={(kind) => bus.emit(kind)}
      onundo={() => step(-1)}
      onredo={() => step(1)}
      onzoom={(direction) => (zoom = stepZoom(scale, direction))}
      onview={(value) => (zoom = value)}
      onpanel={() => (panelOpen = !panelOpen)}
    />
  </div>
  <div class="strip">
    <Notice visible={!!failure} tone="danger" role="alert">{failure}</Notice>
    {@render notice?.()}
  </div>
{/snippet}

<div class="editor-root">
  <EditorStage
    class="bb-editor-stage--compact-panels bb-editor-stage--docked"
    height="100dvh"
    topInset="0px"
    {topStart}
    {topEnd}
    leftPanel={showPanel ? leftPanel : undefined}
    children={board}
  />
</div>

<AddMenu bind:open={addOpen} anchor={addAnchor} {full} onpick={addKind} />

<PickerPanel bind:open={settingsOpen} anchor={settingsAnchor} label={t('overlayEditor.ui.overlaySettings')} placement="below" maxHeight={360}>
  <p class="menu-title">{t('overlayEditor.ui.canvas')}</p>
  <ul class="options">
    {#each sizes as size (sizeLabel(size))}
      <PickerOption as="li" label={sizeLabel(size)} selected={isCurrent(size)} onclick={() => changeSize(size)} />
    {/each}
  </ul>
</PickerPanel>

<style>
  .editor-root {
    display: contents;
    --editor-panel-left: 272px;
  }
  :global(:root) {
    --overlay-bar: calc(56px + env(safe-area-inset-top, 0px));
    --overlay-board-left: 0px;
    --overlay-pad-left: 16px;
  }
  @media (min-width: 1280px) {
    :global(:root) {
      --overlay-board-left: 273px;
      --overlay-pad-left: 289px;
    }
  }

  .canvas-host {
    position: absolute;
    inset: var(--overlay-bar) 0 0 var(--overlay-board-left);
  }
  .canvas-host :global(.canvas-fill) {
    width: 100%;
    height: 100%;
    padding: 0;
    border-radius: 0;
    box-sizing: border-box;
  }

  .add-slot { position: absolute; left: var(--overlay-pad-left); bottom: 16px; z-index: 6; }
  .add-slot :global(.fab) { width: 48px; height: 48px; padding: 0; border-radius: 50%; --btn-pad: 0; box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55); }
  .dock {
    position: absolute;
    left: calc(var(--overlay-pad-left) + 56px);
    right: 16px;
    bottom: 16px;
    z-index: 6;
    display: flex;
    justify-content: center;
    pointer-events: none;
  }
  .dock > :global(*) { pointer-events: auto; }
  .strip {
    position: absolute;
    left: var(--overlay-pad-left);
    right: 16px;
    bottom: 84px;
    z-index: 6;
    display: flex;
    flex-direction: column;
    align-items: center;
    pointer-events: none;
  }
  .strip > :global(*) { width: min(560px, 100%); pointer-events: auto; }
  @media (min-width: 1280px) {
    .dock { left: var(--overlay-pad-left); }
  }
  @media (max-width: 760px) {
    :global(:root) { --overlay-dock-room: env(safe-area-inset-bottom, 0px); }
    .dock { left: 16px; bottom: var(--overlay-dock-room); }
    .add-slot { bottom: calc(84px + var(--overlay-dock-room)); }
    .strip { bottom: calc(140px + var(--overlay-dock-room)); }
  }

  .back {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: var(--bb-radius-sm);
    color: rgba(var(--bb-white-rgb), 0.92);
    transition: background var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .back:hover,
  .back:focus-visible { background: rgba(var(--bb-white-rgb), 0.08); }
  .overlay-name { display: block; min-width: 120px; width: clamp(160px, 28vw, 360px); flex: 0 1 auto; }
  .overlay-name :global(.name-field) {
    padding: 6px 2px;
    border: 0;
    border-bottom: 1px solid rgba(var(--bb-white-rgb), 0.3);
    border-radius: 0;
    background: none;
    box-shadow: none;
    font-size: 16px;
    color: var(--bb-white);
  }
  .overlay-name :global(.name-field:focus-within) { border-bottom-color: var(--bb-white); box-shadow: none; }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--bb-tan);
    opacity: 0;
    transition: opacity var(--bb-dur-fast);
  }
  .dot.on { opacity: 1; }
  .anchor { display: block; }
  :global(.overlay-save) { width: 112px; --btn-pad: 0 12px; --btn-justify: center; }
  .save-check { display: none; flex: none; }
  .save-check.on { display: inline-flex; }
  @media (max-width: 760px) {
    .overlay-name { flex: 1; min-width: 0; width: auto; }
    :global(.overlay-save) { width: 72px; }
  }

  .menu-title { margin: 0; padding: 0 6px; font-family: var(--bb-font-body); font-size: 12px; font-weight: 600; color: var(--bb-muted-light); }
  .options { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
</style>
