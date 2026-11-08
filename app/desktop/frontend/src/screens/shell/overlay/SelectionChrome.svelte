<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import { HANDLES, type Handle } from '../../../lib/overlay/geometry';

  const TOOLBAR_W = 116;
  const TOOLBAR_H = 40;
  const TOOLBAR_GAP = 10;

  let {
    rect,
    roomWidth,
    topLimit,
    busy,
    readout,
    onHandleDown,
    onSettings,
    onDuplicate,
    onRemove,
  }: {
    rect: { left: number; top: number; width: number; height: number };
    roomWidth: number;
    topLimit: number;
    busy: boolean;
    readout: string;
    onHandleDown: (event: PointerEvent, handle: Handle) => void;
    onSettings: () => void;
    onDuplicate: () => void;
    onRemove: () => void;
  } = $props();

  const toolbarAbove = $derived(rect.top - TOOLBAR_H - TOOLBAR_GAP >= topLimit);
  const toolbarTop = $derived(
    toolbarAbove ? rect.top - TOOLBAR_H - TOOLBAR_GAP : rect.top + rect.height + TOOLBAR_GAP,
  );
  const toolbarLeft = $derived(
    Math.max(8, Math.min(roomWidth - TOOLBAR_W - 8, rect.left + rect.width / 2 - TOOLBAR_W / 2)),
  );
</script>

<div
  class="box"
  style:left="{rect.left}px"
  style:top="{rect.top}px"
  style:width="{rect.width}px"
  style:height="{rect.height}px"
>
  {#each HANDLES as handle (handle)}
    <span class="handle handle--{handle}" role="presentation" onpointerdown={(event) => onHandleDown(event, handle)}
    ></span>
  {/each}
</div>

{#if busy}
  <span class="readout" style:left="{toolbarLeft}px" style:top="{toolbarTop}px">{readout}</span>
{:else}
  <div class="toolbar" style:left="{toolbarLeft}px" style:top="{toolbarTop}px">
    <IconButton label="Layer settings" tooltip size="sm" onclick={onSettings}><Icon name="settings" size={15} /></IconButton>
    <IconButton label="Duplicate layer" tooltip size="sm" onclick={onDuplicate}><Icon name="copy" size={15} /></IconButton>
    <IconButton label="Remove layer" tooltip size="sm" onclick={onRemove}><Icon name="trash" size={15} /></IconButton>
  </div>
{/if}

<style>
  .box {
    position: absolute;
    z-index: 3;
    pointer-events: none;
  }

  .handle {
    position: absolute;
    width: 10px;
    height: 10px;
    margin: -5px 0 0 -5px;
    border-radius: var(--bb-radius-xs);
    background: var(--bb-tan-light);
    box-shadow: 0 0 0 1px rgba(var(--bb-shadow-rgb), 0.8);
    pointer-events: auto;
    touch-action: none;
  }

  .handle::after {
    content: '';
    position: absolute;
    inset: -6px;
  }

  .handle--nw { left: 0; top: 0; cursor: nwse-resize; }
  .handle--n { left: 50%; top: 0; cursor: ns-resize; }
  .handle--ne { left: 100%; top: 0; cursor: nesw-resize; }
  .handle--e { left: 100%; top: 50%; cursor: ew-resize; }
  .handle--se { left: 100%; top: 100%; cursor: nwse-resize; }
  .handle--s { left: 50%; top: 100%; cursor: ns-resize; }
  .handle--sw { left: 0; top: 100%; cursor: nesw-resize; }
  .handle--w { left: 0; top: 50%; cursor: ew-resize; }

  .toolbar,
  .readout {
    position: absolute;
    z-index: 3;
    height: 40px;
    box-sizing: border-box;
    border-radius: var(--bb-radius-sm);
    background: var(--bb-bg-1);
    box-shadow: 0 8px 24px rgba(var(--bb-shadow-rgb), 0.55);
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: 2px;
    width: 116px;
    justify-content: center;
  }

  .readout {
    display: inline-flex;
    align-items: center;
    padding: 0 10px;
    color: var(--bb-white);
    font-family: var(--bb-font-mono);
    font-size: 11px;
    white-space: nowrap;
    pointer-events: none;
  }
</style>
