<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from './FlowIcon.svelte';
  import CanvasCard from '@bagel/ui/svelte/CanvasCard.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import FlowAddButton from './FlowAddButton.svelte';
  import { NODE_W, outputsFor, specFor, type FlowNode } from '../../flow/model';
  import { HEADER_H, ROW_H, inputY, nodeHeight, portY } from '../../flow/layout';
  import { describe, portLabel } from '../../flow/describe';
  import { stepLabel } from '../../flow/copy';
  import { getI18n, type MessageKey } from '@bagel/kit';

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
  const { t } = getI18n();
  const line = $derived(describe(t, node));
  const title = $derived(spec ? stepLabel(t, spec) : '');
  const kindLabel = $derived(spec ? t(`flows.board.kind.${spec.kind}` as MessageKey) : '');
</script>

{#snippet outPort(id: string, label: string, y: number, tone: 'yes' | 'no')}
  <span class="out" style:top="{y}px">
    <span class="port port--out" data-port={id} data-tone={tone}></span>
    <span class="add" data-tone={tone}>
      <FlowAddButton
        {label}
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
  <CanvasCard
    class="node"
    background
    {selected}
    data-kind={spec.kind}
    style="width: {NODE_W}px; height: {nodeHeight(spec)}px;"
    role="button"
    tabindex={tabbable ? 0 : -1}
    aria-pressed={selected}
    aria-label={t('flows.board.nodeLabel', { label: title, kind: kindLabel, line })}
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
          <span class="title">{title}</span>
        </div>
        <span class="line">{line}</span>
      </div>
    </div>

    {#if branching}
      <div class="rows" style:top="{HEADER_H}px">
        {#each outputs as out, i (out.id)}
          <div class="row" style:height="{ROW_H}px" data-tone={i === 0 ? 'yes' : 'no'}>
            <span class="row-label">{portLabel(t, node, out.id)}</span>
          </div>
        {/each}
      </div>
      {#each outputs as out, i (out.id)}
        {@render outPort(out.id, t('flows.board.addAfter', { label: portLabel(t, node, out.id) }), portY(spec, out.id), i === 0 ? 'yes' : 'no')}
      {/each}
    {:else}
      {@render outPort(outputs[0]?.id ?? 'next', t('flows.board.addAfterThis'), HEADER_H / 2, 'yes')}
    {/if}
  </CanvasCard>
{/if}

<style>
  /* Background, idle/hover shadow and the selected/focus ring live in the
     shared CanvasCard (ui/styles/elements/canvas-card.css); this file only
     owns the node's own layout, accent tint and drag affordance. */
  :global(.node) {
    --accent: var(--bb-muted-light);
    cursor: grab;
    user-select: none;
    font-family: var(--bb-font-body);
  }
  :global(.node[data-kind='trigger']) {
    --accent: var(--bb-green-glow);
    --accent-rgb: var(--bb-green-glow-rgb);
  }
  :global(.node[data-kind='condition']) {
    --accent: var(--bb-tan);
    --accent-rgb: var(--bb-tan-rgb);
  }
  :global(.node[data-kind='reply']) {
    --accent-rgb: var(--bb-muted-rgb);
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
  :global(.node[data-kind='condition']) .title {
    font-size: 14px;
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  :global(.node[data-kind='condition']) .line {
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
  :global(.node):hover::after {
    content: '';
    position: absolute;
    top: -8px;
    bottom: -8px;
    left: 100%;
    width: 48px;
  }
  :global(.node):hover .add,
  .out:focus-within .add {
    opacity: 1;
    pointer-events: auto;
  }
  :global(.canvas[data-touch]) .out::before,
  :global(.canvas[data-touch] .node):hover::after {
    display: none;
  }
  :global(.canvas[data-touch] .node):not(.selected) .out {
    pointer-events: none;
  }
  :global(.canvas[data-touch] .node):not(.selected):hover .add {
    opacity: 0;
    pointer-events: none;
  }
  :global(.canvas[data-touch] .node.selected) .add {
    opacity: 1;
    pointer-events: auto;
    transform-origin: left center;
    scale: calc(var(--inv, 1) * 1.25);
  }
  :global(.canvas[data-touch] .node.selected) .add[data-tone='yes'] {
    translate: 0 calc(-1 * max(0px, 22px * var(--inv, 1) - 16px));
  }
  :global(.canvas[data-touch] .node.selected) .add[data-tone='no'] {
    translate: 0 max(0px, 22px * var(--inv, 1) - 16px);
  }
  :global(.canvas[data-touch] .node.selected) .port--out::before {
    inset: calc((8px - 46px * var(--inv, 1)) / 2);
  }
  :global(.canvas[data-touch][data-connecting]) .add {
    opacity: 0;
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.node),
    .add,
    .port {
      transition: none;
    }
  }
</style>
