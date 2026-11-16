<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import PickerOption from '@bagel/ui/svelte/PickerOption.svelte';
  import { portal } from '@bagel/ui/lib/overlay-stack';
  import '@bagel/ui/styles/elements/picker-panel.css';

  let { id, anchor, items, active, label, onselect }: {
    id: string;
    anchor: HTMLElement;
    items: { token: string; label: string }[];
    active: number;
    label: string;
    onselect: (token: string) => void;
  } = $props();
  let list = $state<HTMLElement>();
  let position = $state({ left: 0, top: 0, width: 0 });

  function place() {
    const bounds = anchor.getBoundingClientRect();
    const bottom = visualViewport?.height ?? innerHeight;
    const top = bounds.bottom + 200 < bottom ? bounds.bottom + 4 : Math.max(8, bounds.top - 200);
    position = { left: bounds.left, top, width: bounds.width };
  }
  $effect(() => {
    void anchor;
    place();
    window.addEventListener('scroll', place, true);
    visualViewport?.addEventListener('resize', place);
    return () => {
      window.removeEventListener('scroll', place, true);
      visualViewport?.removeEventListener('resize', place);
    };
  });
  $effect(() => {
    list?.querySelector(`[id="${id}-suggestion-${active}"]`)?.scrollIntoView({ block: 'nearest' });
  });
</script>

<div class="bb-picker-panel bb-picker-panel--dropdown suggestions" id="{id}-suggestions" role="listbox" tabindex="-1" aria-label={label} bind:this={list} style:left="{position.left}px" style:top="{position.top}px" style:width="{position.width}px" use:portal onpointerdown={(event) => event.preventDefault()}>
  {#each items as item, index (item.token)}
    <PickerOption id="{id}-suggestion-{index}" role="option" aria-selected={index === active} aria-current={index === active} tabindex={-1} onclick={() => onselect(item.token)}>
      <span class="suggestion-label" data-fit><bb-fit>{item.label}</bb-fit></span>
      <span class="suggestion-token">{item.token}</span>
    </PickerOption>
  {/each}
</div>

<style>
  .suggestions { max-height: 196px; gap: 2px; padding: 6px; }
  .suggestion-label { flex: 1; min-width: 0; white-space: nowrap; font-size: 13px; }
  .suggestion-token { flex: none; font-size: 11px; color: var(--bb-muted-light); }
</style>
