<script module lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { OverlayWidget } from '../../lib/overlay-document';

  export type LayerActions = {
    select: (id: string) => void;
    patch: (id: string, change: Partial<OverlayWidget>) => void;
    reorder: (id: string, offset: -1 | 1) => void;
    move: (id: string, to: number) => void;
    duplicate: () => void;
    remove: () => void;
  };
</script>

<script lang="ts">
  import { tick } from 'svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Input from '@bagel/ui/svelte/Input.svelte';
  import { getI18n } from '../../lib/i18n/context';
  import type { OverlayDocument } from '../../lib/overlay-document';
  import { kindIcon } from '../../lib/overlay-editor/kinds';
  import Fit from './Fit.svelte';
  import Glyph from './Glyph.svelte';

  let { doc, selected, full, actions }: {
    doc: OverlayDocument;
    selected: string;
    full: boolean;
    actions: LayerActions;
  } = $props();

  const { t } = getI18n();
  let editing = $state('');
  let draft = $state('');
  let dragging = $state('');
  let over = $state(-1);
  let listEl = $state<HTMLElement>();

  const layers = $derived(doc.widgets.toReversed());
  const position = $derived(layers.findIndex((layer) => layer.id === selected));
  const hasSelection = $derived(position >= 0);

  async function startRename(id: string, name: string): Promise<void> {
    editing = id;
    draft = name;
    await tick();
    const field = listEl?.querySelector<HTMLInputElement>('.renaming input');
    field?.focus();
    field?.select();
  }

  function commit(): void {
    const id = editing;
    const name = draft.trim();
    editing = '';
    if (id && name) actions.patch(id, { name });
  }

  function renameKey(event: KeyboardEvent): void {
    if (event.key === 'Enter') commit();
    if (event.key !== 'Escape') return;
    event.stopPropagation();
    editing = '';
  }

  function dragStart(event: DragEvent, id: string): void {
    dragging = id;
    event.dataTransfer?.setData('text/plain', id);
  }

  function dragOver(event: DragEvent, index: number): void {
    if (!dragging) return;
    event.preventDefault();
    over = index;
  }

  function drop(event: DragEvent, index: number): void {
    event.preventDefault();
    const id = dragging;
    dragEnd();
    if (id) actions.move(id, layers.length - 1 - index);
  }

  function dragEnd(): void {
    dragging = '';
    over = -1;
  }
</script>

<div class="tools" role="toolbar" aria-label={t('overlayEditor.ui.layerActions')}>
  <IconButton label={t('overlayEditor.ui.raise')} size="sm" tooltip disabled={!hasSelection || position === 0} onclick={() => actions.reorder(selected, 1)}>
    <Icon name="chevron" size={14} class="chev-up" />
  </IconButton>
  <IconButton label={t('overlayEditor.ui.lower')} size="sm" tooltip disabled={!hasSelection || position === layers.length - 1} onclick={() => actions.reorder(selected, -1)}>
    <Icon name="chevron" size={14} />
  </IconButton>
  <IconButton label={t('overlayEditor.ui.duplicate')} size="sm" tooltip disabled={!hasSelection || full} onclick={actions.duplicate}>
    <Icon name="copy" size={14} />
  </IconButton>
  <IconButton label={t('common.delete')} size="sm" tone="danger" tooltip disabled={!hasSelection} onclick={actions.remove}>
    <Icon name="trash" size={14} />
  </IconButton>
</div>
<ul class="list" bind:this={listEl}>
  {#each layers as layer, index (layer.id)}
    <li
      class="row"
      class:selected={layer.id === selected}
      class:dim={layer.hidden}
      class:drop={over === index && dragging !== layer.id}
      draggable={editing !== layer.id}
      ondragstart={(event) => dragStart(event, layer.id)}
      ondragover={(event) => dragOver(event, index)}
      ondrop={(event) => drop(event, index)}
      ondragend={dragEnd}
    >
      {#if editing === layer.id}
        <div class="row-main renaming">
          <IconTile size="sm" style="--tile: 28px"><Icon name={kindIcon(layer.kind)} size={14} /></IconTile>
          <Input fill maxlength={120} aria-label={t('overlayEditor.ui.layerName')} bind:value={draft} onkeydown={renameKey} onblur={commit} />
        </div>
      {:else}
        <button type="button" class="row-main" style="--bb-cursor: quiet" aria-pressed={layer.id === selected} onclick={() => actions.select(layer.id)} ondblclick={() => startRename(layer.id, layer.name)}>
          <IconTile size="sm" style="--tile: 28px"><Icon name={kindIcon(layer.kind)} size={14} /></IconTile>
          <span class="name">{layer.name}</span>
        </button>
      {/if}
      <div class="row-actions">
        <IconButton class={layer.hidden ? 'quiet on' : 'quiet'} label={t('overlayEditor.ui.hideLayer')} size="sm" tooltip aria-pressed={!!layer.hidden} onclick={() => actions.patch(layer.id, { hidden: !layer.hidden })}>
          <Glyph name={layer.hidden ? 'eyeOff' : 'eye'} size={14} />
        </IconButton>
        <IconButton class={layer.locked ? 'quiet on' : 'quiet'} label={t('overlayEditor.ui.lockLayer')} size="sm" tooltip aria-pressed={!!layer.locked} onclick={() => actions.patch(layer.id, { locked: !layer.locked })}>
          <Glyph name={layer.locked ? 'lock' : 'unlock'} size={14} />
        </IconButton>
      </div>
    </li>
  {:else}
    <li class="empty"><Fit>{t('overlayEditor.ui.noLayers')}</Fit></li>
  {/each}
</ul>

<style>
  .tools { display: flex; justify-content: flex-end; gap: 2px; padding-bottom: 6px; }
  .tools :global(.chev-up) { transform: rotate(180deg); }
  .list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
  .row { display: flex; align-items: center; gap: 2px; border-radius: var(--bb-radius-sm); transition: background var(--bb-dur-fast) ease; }
  .row.selected { background: rgba(var(--bb-white-rgb), 0.1); }
  .row:not(.selected):hover { background: rgba(var(--bb-white-rgb), 0.05); }
  .row.drop { box-shadow: inset 0 2px 0 var(--bb-green-glow); }
  .row.dim .name { opacity: 0.5; }
  .row-main { flex: 1; min-width: 0; display: flex; align-items: center; gap: 10px; padding: 6px; border: 0; background: none; color: inherit; font: inherit; text-align: left; cursor: pointer; }
  .renaming { cursor: default; }
  .row-main:focus-visible { outline: 2px solid var(--bb-green-glow); outline-offset: -2px; border-radius: var(--bb-radius-sm); }
  .name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 1.3; font-size: 13.5px; color: var(--bb-white); }
  .row-actions { flex: none; display: flex; align-items: center; padding-inline-end: 4px; }
  .row-actions :global(.quiet) { opacity: 0; transition: opacity var(--bb-dur-fast) ease; }
  .row:hover .row-actions :global(.quiet),
  .row:focus-within .row-actions :global(.quiet),
  .row-actions :global(.quiet.on) { opacity: 1; }
  @media (hover: none) {
    .row-actions :global(.quiet) { opacity: 1; }
  }
  .empty { padding: 12px 6px; font-size: 13.5px; color: var(--bb-muted); }
</style>
