<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import { KIND_ICON, KIND_TINT } from '../../../lib/overlay/kind';
  import type { Layer } from '../../../lib/overlay/types';

  let {
    layers,
    selectedId,
    onSelect,
    onMoveUp,
    onMoveDown,
    onRemove,
    onClose,
  }: {
    layers: Layer[];
    selectedId: string | null;
    onSelect: (id: string) => void;
    onMoveUp: (id: string) => void;
    onMoveDown: (id: string) => void;
    onRemove: (id: string) => void;
    /** Only set where the panel is an on-demand overlay (narrow widths); docked, it has no close. */
    onClose?: () => void;
  } = $props();

  // Front-most layer first, matching the stage's visual stacking order.
  const stacked = $derived([...layers].reverse());
</script>

<div class="panel">
  <header class="head">
    <span class="title">Layers</span>
    {#if onClose}
      <IconButton label="Close layers" size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
    {/if}
  </header>

  <ul class="list">
    {#each stacked as layer, i (layer.id)}
      <li class="row" class:selected={layer.id === selectedId}>
        <button type="button" class="row-main" onclick={() => onSelect(layer.id)}>
          <IconTile size="sm" style="--tile: 28px; color: {KIND_TINT[layer.kind]}">
            <Icon name={KIND_ICON[layer.kind]} size={14} />
          </IconTile>
          <span class="name">{layer.name}</span>
        </button>
        <div class="row-actions">
          <IconButton label="Move layer up" size="sm" tooltip disabled={i === 0} onclick={() => onMoveUp(layer.id)}>
            <Icon name="chevron" size={12} class="chev-up" />
          </IconButton>
          <IconButton
            label="Move layer down"
            size="sm"
            tooltip
            disabled={i === stacked.length - 1}
            onclick={() => onMoveDown(layer.id)}
          >
            <Icon name="chevron" size={12} />
          </IconButton>
          <IconButton label="Remove layer" size="sm" tone="danger" tooltip onclick={() => onRemove(layer.id)}>
            <Icon name="trash" size={12} />
          </IconButton>
        </div>
      </li>
    {:else}
      <li class="empty">No layers yet.</li>
    {/each}
  </ul>
</div>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }

  .head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 16px 12px 10px 16px;
  }

  .title {
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: var(--bb-white);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: 4px 10px 14px;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .row {
    display: flex;
    align-items: flex-start;
    gap: 4px;
    border-radius: var(--bb-radius-sm);
    transition: background var(--bb-dur-fast) ease;
  }

  .row.selected {
    background: rgba(var(--bb-white-rgb), 0.1);
  }

  .row:not(.selected):hover {
    background: rgba(var(--bb-white-rgb), 0.05);
  }

  .row-main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 6px;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .row-main:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: -2px;
    border-radius: var(--bb-radius-sm);
  }

  .name {
    min-width: 0;
    overflow-wrap: anywhere;
    line-height: 1.3;
    font-size: 13.5px;
    color: var(--bb-white);
  }

  .row-actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 6px 8px 6px 0;
  }

  .row-actions :global(.chev-up) {
    transform: rotate(180deg);
  }

  .empty {
    padding: 16px 10px;
    font-size: 13.5px;
    color: var(--bb-muted);
  }
</style>
