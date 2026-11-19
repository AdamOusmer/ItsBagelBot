<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from './FlowIcon.svelte';
  import CanvasCard from '@bagel/ui/svelte/CanvasCard.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import IconTile from '@bagel/ui/svelte/IconTile.svelte';
  import FlowAddButton from './FlowAddButton.svelte';
  import { NODE_W, outputsFor, specFor, type FlowNode } from '../../flow/model';
  import { HEADER_H, ROW_H, inputY, nodeHeight, portY } from '../../flow/layout';
  import { describe, portLabel } from '../../flow/describe';
  import { stepLabel } from '../../flow/copy';
  import { getI18n, type MessageKey } from '@bagel/kit';

  let {
    node,
    trigger,
    selected = false,
    tabbable = true,
    touch = false,
    connecting = false,
    parallel = [],
    onadd
  }: {
    node: FlowNode;
    trigger?: FlowNode;
    selected?: boolean;
    tabbable?: boolean;
    touch?: boolean;
    connecting?: boolean;
    parallel?: string[];
    onadd?: (port: string, anchor: HTMLElement) => void;
  } = $props();

  let ports = $state<Record<string, HTMLElement | null>>({});
  let adds = $state<Record<string, HTMLElement | null>>({});

  export function portAt(el: Element | null): string | undefined {
    return Object.keys(ports).find((id) => ports[id]?.contains(el));
  }

  export function addButton(port: string): HTMLButtonElement | null {
    return adds[port]?.querySelector('button') ?? null;
  }

  const spec = $derived(specFor(node.type));
  const outputs = $derived(outputsFor(spec, node));
  const isTrigger = $derived(spec?.kind === 'trigger');
  const branching = $derived(outputs.length > 1);
  const { t } = getI18n();
  const line = $derived(describe(t, node, trigger));
  const title = $derived(spec ? stepLabel(t, spec) : '');
  const kindLabel = $derived(spec ? t(`flows.board.kind.${spec.kind}` as MessageKey) : '');
  const off = $derived(spec?.kind === 'reply' && node.config.disabled === true);
  const offLabel = $derived(t('flows.board.stepOff'));
  const nodeLabel = $derived(t('flows.board.nodeLabel', { label: title, kind: kindLabel, line }));
  const cardClass = $derived(['node', spec && `kind-${spec.kind}`, touch && 'touch', connecting && 'connecting'].filter(Boolean).join(' '));
</script>

{#snippet outPort(id: string, label: string, y: number, tone: 'yes' | 'no')}
  <span class="out" style:top="{y}px">
    {#if parallel.includes(id)}
      <span class="parallel" style:top="{branching ? -5 : -HEADER_H / 2 - 22}px">
        <Icon name="branch" size={12} />
        <span class="parallel-text bb-fit-box"><bb-fit>{t('flows.board.atSameTime')}</bb-fit></span>
      </span>
    {/if}
    <span class="port port--out tone-{tone}" bind:this={ports[id]}></span>
    <span class="add tone-{tone}" bind:this={adds[id]}>
      <FlowAddButton
        {touch}
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
    class={cardClass}
    background
    {selected}
    style="width: {NODE_W}px; height: {nodeHeight(spec, node)}px;"
    role="button"
    tabindex={tabbable ? 0 : -1}
    aria-pressed={selected}
    aria-label={off ? `${nodeLabel}, ${offLabel}` : nodeLabel}
  >
    {#if !isTrigger}
      <span class="port port--in" style:top="{inputY}px"></span>
    {/if}

    <div class="head" style:height="{HEADER_H}px">
      <IconTile class="tile">
        <Icon name={spec.icon} size={18} />
      </IconTile>
      <div class="text">
        <div class="title-row">
          <span class="title">{title}</span>
        </div>
        {#if isTrigger}
          <span class="line trigger-line bb-fit-box"><bb-fit mode="block">{line}</bb-fit></span>
        {:else}
          <span class="line">{line}</span>
        {/if}
      </div>
    </div>

    <span class="off-tag" class:off aria-hidden="true">
      <Chip as="span" tone="muted" class="bb-fit-box" style="min-width: 0; justify-content: center; padding: 2px 4px">
        <bb-fit>{offLabel}</bb-fit>
      </Chip>
    </span>

    {#if branching}
      <div class="rows" style:top="{HEADER_H}px">
        {#each outputs as out, i (out.id)}
          <div class="row" class:tone-yes={i !== 1} style:height="{ROW_H}px">
            <span class="row-label">{portLabel(t, node, out.id)}</span>
          </div>
        {/each}
      </div>
      {#each outputs as out, i (out.id)}
        {@render outPort(out.id, t('flows.board.addAfter', { label: portLabel(t, node, out.id) }), portY(spec, out.id, node), i === 1 ? 'no' : 'yes')}
      {/each}
    {:else if outputs[0]}
      {@render outPort(outputs[0].id, t('flows.board.addAfterThis'), HEADER_H / 2, 'yes')}
    {/if}
  </CanvasCard>
{/if}

<style>
  :global(.node) {
    --accent: var(--bb-muted-light);
    cursor: grab;
    user-select: none;
    font-family: var(--bb-font-body);
  }
  :global(.node.kind-trigger) {
    --accent: var(--bb-green-glow);
    --accent-rgb: var(--bb-green-glow-rgb);
  }
  :global(.node.kind-condition) {
    --accent: var(--bb-tan);
    --accent-rgb: var(--bb-tan-rgb);
  }
  :global(.node.kind-reply) {
    --accent-rgb: var(--bb-muted-rgb);
  }
  .head {
    box-sizing: border-box;
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 14px 56px 0 14px;
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
  .trigger-line {
    display: block;
    height: 36px;
  }
  .off-tag {
    position: absolute;
    top: 8px;
    right: 8px;
    display: grid;
    width: 44px;
    height: 20px;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .off-tag.off { opacity: 1; }
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
    padding: 0 56px 0 14px;
  }
  .row-label {
    font-size: 14px;
    font-weight: 500;
    line-height: 18px;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  .row.tone-yes .row-label {
    color: var(--bb-green-glow);
  }
  :global(.node.kind-condition) .title {
    font-size: 14px;
    font-weight: 500;
    color: rgba(var(--bb-white-rgb), 0.92);
  }
  :global(.node.kind-condition) .line {
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
  .parallel {
    position: absolute;
    right: 26px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    box-sizing: border-box;
    width: 166px;
    height: 22px;
    padding: 3px 7px;
    border-radius: var(--bb-radius-pill);
    background: var(--bb-bg-1);
    color: var(--bb-white);
    font-size: 12px;
    line-height: 16px;
    pointer-events: none;
  }
  .parallel-text {
    display: block;
    width: 136px;
    min-width: 0;
    white-space: nowrap;
  }
  .out {
    position: absolute;
    right: -6px;
    width: 12px;
    height: 12px;
    margin-top: -6px;
  }
  .port--out {
    position: absolute;
    left: 0;
    top: 6px;
    z-index: 1;
    cursor: crosshair;
  }
  .port--out.tone-yes {
    border-color: var(--bb-green-glow);
  }
  .port--out:hover,
  .out:focus-within .port--out {
    background: var(--bb-muted);
  }
  .port--out.tone-yes:hover,
  .out:focus-within .port--out.tone-yes {
    background: var(--bb-green-glow);
  }
  .add {
    position: absolute;
    left: -38px;
    top: -6px;
    z-index: 2;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  :global(.node):hover .add,
  :global(.node.selected) .add,
  .out:focus-within .add {
    opacity: 1;
    pointer-events: auto;
  }
  :global(.node.touch):not(.selected) .out {
    pointer-events: none;
  }
  :global(.node.touch):not(.selected):hover .add {
    opacity: 0;
    pointer-events: none;
  }
  :global(.node.touch.selected) .add {
    left: 26px;
    opacity: 1;
    pointer-events: auto;
    transform-origin: left center;
    scale: calc(var(--inv, 1) * 1.375);
  }
  :global(.node.touch.selected) .add.tone-yes {
    translate: 0 calc(-48px * var(--inv, 1));
  }
  :global(.node.touch.selected) .add.tone-no {
    translate: 0 calc(48px * var(--inv, 1));
  }
  :global(.node.touch.selected) .port--out::before {
    inset: calc((8px - 46px * var(--inv, 1)) / 2);
  }
  :global(.node.touch.connecting) .add {
    opacity: 0;
    pointer-events: none;
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.node),
    .add,
    .off-tag,
    .port {
      transition: none;
    }
  }
</style>
