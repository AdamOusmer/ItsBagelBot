<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { onMount, tick } from 'svelte';
  import Icon from './FlowIcon.svelte';
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import Scroller from '@bagel/ui/svelte/Scroller.svelte';
  import SearchInput from '@bagel/ui/svelte/SearchInput.svelte';
  import { NODE_SPECS, type FlowKind, type NodeSpec } from '#lib/flow/model';
  import { stepFocus } from '#lib/flow/keyboard';

  export type LibraryMode = FlowKind | 'search';

  let {
    mode,
    focusToken = 0,
    onAdd,
    onClose
  }: { mode: LibraryMode; focusToken?: number; onAdd: (type: string) => void; onClose: () => void } = $props();

  const HEADINGS: Record<FlowKind, { title: string; hint: string }> = {
    trigger: { title: 'When', hint: 'What starts this system' },
    condition: { title: 'Only if', hint: 'Keep going only when this is true' },
    reply: { title: 'Do', hint: 'What happens next' }
  };
  const TINTS: Record<FlowKind, string> = {
    trigger: 'var(--bb-green-glow)',
    condition: 'var(--bb-tan)',
    reply: 'var(--bb-muted-light)'
  };
  const SOURCE_ORDER = ['Twitch', 'Discord', 'Modules', 'Basics'];

  const sourceGroup = (source: string) =>
    source.includes('Twitch') ? 'Twitch' : source.includes('Discord') ? 'Discord' : source.includes('Core') ? 'Basics' : 'Modules';

  let query = $state('');
  let searchEl = $state<HTMLInputElement>();
  let root = $state<HTMLElement>();

  onMount(() => {
    if (mode === 'search') searchEl?.focus();
  });

  $effect(() => {
    if (focusToken > 0) void tick().then(() => (searchEl ?? root?.querySelector<HTMLElement>('.row'))?.focus());
  });

  const onKeyDown = (e: KeyboardEvent) => stepFocus(e, Array.from(root?.querySelectorAll<HTMLElement>('input, .row') ?? []));

  const sections = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const pool = NODE_SPECS.filter(
      (s) =>
        (mode === 'search' || s.kind === mode) &&
        (!q || `${s.label} ${s.summary} ${s.source}`.toLowerCase().includes(q))
    );
    return SOURCE_ORDER.map((title) => ({ title, specs: pool.filter((s) => sourceGroup(s.source) === title) })).filter(
      (g) => g.specs.length > 0
    );
  });

  function onDragStart(e: DragEvent, spec: NodeSpec) {
    if (!e.dataTransfer) return;
    e.dataTransfer.setData('application/x-flow-node', spec.type);
    e.dataTransfer.effectAllowed = 'copy';
    setTimeout(onClose, 0);
  }
</script>

<div class="library" role="presentation" bind:this={root} onkeydown={onKeyDown}>
  <header class="head">
    {#if mode === 'search'}
      <div class="search">
        <SearchInput bind:value={query} bind:element={searchEl} placeholder="Search all steps" fill />
      </div>
    {:else}
      <div class="titles">
        <span class="title">{HEADINGS[mode].title}</span>
        <span class="hint">{HEADINGS[mode].hint}</span>
      </div>
    {/if}
    <IconButton label="Close library" size="sm" onclick={onClose}><Icon name="x" size={14} /></IconButton>
  </header>

  <Scroller fill padding="0 8px 16px" smooth>
    {#each sections as group (group.title)}
      <section class="group">
        <h3 class="group-title">{group.title}</h3>
        {#each group.specs as spec (spec.type)}
          <button
            class="row"
            type="button"
            draggable="true"
            ondragstart={(e) => onDragStart(e, spec)}
            onclick={() => onAdd(spec.type)}
          >
            <IconTile size="sm" style="--tile: 32px; color: {TINTS[spec.kind]}"><Icon name={spec.icon} size={16} /></IconTile>
            <span class="copy">
              <span class="name">{spec.label}</span>
              <span class="summary">{spec.summary}</span>
            </span>
          </button>
        {/each}
      </section>
    {:else}
      <p class="empty">No steps match that search.</p>
    {/each}
  </Scroller>
</div>

<style>
  .library {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex: none;
    padding: 16px 12px 12px 16px;
  }
  .search {
    flex: 1;
    min-width: 0;
  }
  .titles {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .title {
    font-family: var(--bb-font-body);
    font-size: 17px;
    font-weight: 600;
    color: var(--bb-white);
  }
  .hint {
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-bottom: 12px;
  }
  .group-title {
    margin: 0;
    padding: 8px 8px 6px;
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 8px;
    border: 0;
    border-radius: 8px;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: grab;
    transition: background 0.15s ease;
  }
  .row:hover,
  .row:focus-visible {
    background: rgba(var(--bb-white-rgb), 0.06);
  }
  .row:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: -2px;
  }
  .row:active {
    cursor: grabbing;
  }
  .copy {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 15px;
    line-height: 1.3;
    color: var(--bb-white);
  }
  .summary {
    font-size: 14px;
    line-height: 1.35;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .empty {
    margin: 0;
    padding: 16px 8px;
    font-size: 14px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
</style>
