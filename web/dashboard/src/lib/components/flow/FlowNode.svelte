<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from './FlowIcon.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import FlowAddButton from './FlowAddButton.svelte';
  import { NODE_W, outputsFor, specFor, type FlowNode } from '../../flow/model';
  import { HEADER_H, ROW_H, inputY, nodeHeight, portY } from '../../flow/layout';
  import { describe, portLabel } from '../../flow/describe';

  let {
    node,
    selected = false,
    tabbable = true,
    onadd
  }: {
    node: FlowNode;
    selected?: boolean;
    tabbable?: boolean;
    onadd?: (port: string, anchor: HTMLElement) => void;
  } = $props();

  const spec = $derived(specFor(node.type));
  const outputs = $derived(spec ? outputsFor(spec) : []);
  const isTrigger = $derived(spec?.kind === 'trigger');
  const branching = $derived(Boolean(spec?.outputs));
  const line = $derived(describe(node));

  const KIND_LABEL = { trigger: 'Trigger', condition: 'Condition', reply: 'Action' } as const;
</script>

{#snippet outPort(id: string, label: string, y: number, tone: 'yes' | 'no')}
  <span class="out" style:top="{y}px">
    <span class="port port--out" data-port={id} data-tone={tone}></span>
    <span class="add" data-tone={tone}>
      <FlowAddButton
        label="Add step after {label}"
        tabindex={tabbable ? 0 : -1}
        onclick={(e) => {
          e.stopPropagation();
          onadd?.(id, e.currentTarget);
        }}
      />
    </span>
  </span>
{/snippet}

{#if spec}
  <div
    class="node"
    class:selected
    data-kind={spec.kind}
    style:width="{NODE_W}px"
    style:height="{nodeHeight(spec)}px"
    role="button"
    tabindex={tabbable ? 0 : -1}
    aria-pressed={selected}
    aria-label="{spec.label}, {KIND_LABEL[spec.kind]}: {line}"
  >
    {#if !isTrigger}
      <span class="port port--in" data-in style:top="{inputY}px"></span>
    {/if}

    <div class="head" style:height="{HEADER_H}px">
      <IconTile class="tile">
        <Icon name={spec.icon} size={18} />
      </IconTile>
      <div class="text">
        <div class="title-row">
          <span class="title">{spec.label}</span>
        </div>
        <span class="line">{line}</span>
      </div>
    </div>

    {#if branching}
      <div class="rows" style:top="{HEADER_H}px">
        {#each outputs as out, i (out.id)}
          <div class="row" style:height="{ROW_H}px" data-tone={i === 0 ? 'yes' : 'no'}>
            <span class="row-label">{portLabel(node, out)}</span>
          </div>
        {/each}
      </div>
      {#each outputs as out, i (out.id)}
        {@render outPort(out.id, out.label, portY(spec, out.id), i === 0 ? 'yes' : 'no')}
      {/each}
    {:else}
      {@render outPort(outputs[0]?.id ?? 'next', 'this step', HEADER_H / 2, 'yes')}
    {/if}
  </div>
{/if}

<style>
  .node {
    --accent: var(--bb-muted-light);
    position: relative;
    box-sizing: border-box;
    border-radius: var(--bb-radius-md);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.07), rgba(var(--bb-white-rgb), 0.03)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-2), 0 10px 28px rgba(var(--bb-shadow-rgb), 0.6);
    cursor: grab;
    user-select: none;
    outline: none;
    font-family: var(--bb-font-body);
    transition:
      box-shadow var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .node[data-kind='trigger'] {
    --accent: var(--bb-green-glow);
    --accent-rgb: var(--bb-green-glow-rgb);
  }
  .node[data-kind='condition'] {
    --accent: var(--bb-tan);
    --accent-rgb: var(--bb-tan-rgb);
  }
  .node[data-kind='reply'] {
    --accent-rgb: var(--bb-muted-rgb);
  }
  .node:hover {
    box-shadow: var(--bb-depth-3), 0 14px 36px rgba(var(--bb-shadow-rgb), 0.75);
  }
  .node.selected {
    box-shadow:
      0 0 0 2px var(--bb-tan-light),
      0 0 28px rgba(var(--bb-tan-light-rgb), 0.18);
  }
  .node:focus-visible {
    box-shadow:
      0 0 0 3px var(--bb-green-glow),
      0 0 28px rgba(var(--bb-green-glow-rgb), 0.3);
  }
  .head {
    box-sizing: border-box;
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 14px 14px 0;
  }
  .head :global(.tile) {
    --tile: 36px;
    --tile-bg: rgba(var(--accent-rgb), 0.14);
    margin-top: 2px;
    color: var(--accent);
  }
  .text {
    min-width: 0;
    flex: 1;
    display: grid;
    gap: 2px;
  }
  .title-row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    height: 22px;
  }
  .title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 15px;
    font-weight: 600;
    line-height: 22px;
    color: var(--bb-white);
  }
  .line {
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
    overflow-wrap: anywhere;
    font-size: 14px;
    line-height: 18px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .rows {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    border-radius: 0 0 var(--bb-radius-md) var(--bb-radius-md);
    background: rgba(var(--bb-shadow-rgb), 0.2);
  }
  .row {
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    padding: 0 20px 0 14px;
  }
  .row-label {
    font-size: 14px;
    font-weight: 500;
    line-height: 18px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .row[data-tone='yes'] .row-label {
    color: var(--bb-green-glow);
  }
  .node[data-kind='condition'] .title {
    font-size: 14px;
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .node[data-kind='condition'] .line {
    font-size: 15px;
    font-weight: 600;
    line-height: 20px;
    color: var(--bb-white);
  }
  .port {
    position: absolute;
    width: 12px;
    height: 12px;
    margin-top: -6px;
    border-radius: var(--bb-radius-pill);
    background: var(--bb-card-bg);
    border: 2px solid var(--bb-muted);
    box-sizing: border-box;
    transition: background-color var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .port::before {
    content: '';
    position: absolute;
    inset: -8px;
    border-radius: var(--bb-radius-pill);
  }
  .port--in {
    left: -6px;
  }
  .out {
    position: absolute;
    right: -6px;
    width: 12px;
    height: 12px;
    margin-top: -6px;
  }
  .out::before {
    content: '';
    position: absolute;
    left: 0;
    top: -10px;
    width: 52px;
    height: 32px;
  }
  .port--out {
    position: absolute;
    left: 0;
    top: 6px;
    z-index: 1;
    cursor: crosshair;
  }
  .port--out[data-tone='yes'] {
    border-color: var(--bb-green-glow);
  }
  .port--out:hover,
  .out:focus-within .port--out {
    background: var(--bb-muted);
  }
  .port--out[data-tone='yes']:hover,
  .out:focus-within .port--out[data-tone='yes'] {
    background: var(--bb-green-glow);
  }
  .add {
    position: absolute;
    left: 22px;
    top: -6px;
    z-index: 2;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .node:hover::after {
    content: '';
    position: absolute;
    top: -8px;
    bottom: -8px;
    left: 100%;
    width: 48px;
  }
  .node:hover .add,
  .out:focus-within .add {
    opacity: 1;
    pointer-events: auto;
  }
  :global(.canvas[data-touch]) .out::before,
  :global(.canvas[data-touch]) .node:hover::after {
    display: none;
  }
  :global(.canvas[data-touch]) .node:not(.selected) .out {
    pointer-events: none;
  }
  :global(.canvas[data-touch]) .node:not(.selected):hover .add {
    opacity: 0;
    pointer-events: none;
  }
  :global(.canvas[data-touch]) .node.selected .add {
    opacity: 1;
    pointer-events: auto;
    transform-origin: left center;
    scale: calc(var(--inv, 1) * 1.25);
  }
  :global(.canvas[data-touch]) .node.selected .add[data-tone='yes'] {
    translate: 0 calc(-1 * max(0px, 22px * var(--inv, 1) - 16px));
  }
  :global(.canvas[data-touch]) .node.selected .add[data-tone='no'] {
    translate: 0 max(0px, 22px * var(--inv, 1) - 16px);
  }
  :global(.canvas[data-touch]) .node.selected .port--out::before {
    inset: calc((8px - 46px * var(--inv, 1)) / 2);
  }
  :global(.canvas[data-touch][data-connecting]) .add {
    opacity: 0;
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .node,
    .add,
    .port {
      transition: none;
    }
  }
</style>
