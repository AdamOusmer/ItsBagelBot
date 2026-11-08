<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Bolota from '@bagel/ui/svelte/Bolota.svelte';
  import Chip from '@bagel/ui/svelte/Chip.svelte';
  import type { Layer } from '../../../lib/overlay/types';

  let { layer, canvasH }: { layer: Layer; canvasH: number } = $props();

  const ALIGN = { start: 'left', center: 'center', end: 'right' } as const;
  const faceSize = $derived(Math.round(canvasH * 0.15));
</script>

{#if layer.kind === 'text'}
  <span
    class="text"
    style:font-size="{(layer.fontSize / 100) * canvasH}px"
    style:color={layer.color}
    style:text-align={ALIGN[layer.align]}>{layer.content}</span
  >
{:else if layer.kind === 'image'}
  <img class="image" style:object-fit={layer.fit} src="/logo.png" alt={layer.caption} draggable="false" />
{:else if layer.kind === 'alert'}
  <div class="alert">
    <Bolota name="yeast" size={faceSize} plate={false} />
    <div class="alert__copy">
      <Chip tone={layer.tone} pressed as="span">{layer.tone}</Chip>
      <strong class="alert__title">{layer.title}</strong>
      <span class="alert__subtitle">{layer.subtitle}</span>
    </div>
  </div>
{:else if layer.kind === 'chat'}
  <div class="chat">
    {#each layer.messages.slice(0, layer.rows) as message}
      <p class="chat__line">
        <span class="chat__user">{message.user}</span>
        <span class="chat__text">{message.message}</span>
      </p>
    {/each}
  </div>
{/if}

<style>
  .text {
    display: block;
    width: 100%;
    overflow: hidden;
    font-family: var(--bb-font-body);
    font-weight: 700;
    line-height: 1.2;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-shadow: 0 2px 12px rgba(var(--bb-shadow-rgb), 0.8);
  }

  .image {
    display: block;
    width: 100%;
    height: 100%;
    padding: calc(var(--canvas-scale) * 12px);
    box-sizing: border-box;
    pointer-events: none;
  }

  .alert {
    display: flex;
    align-items: center;
    gap: calc(var(--canvas-scale) * 14px);
    height: 100%;
    padding: calc(var(--canvas-scale) * 14px) calc(var(--canvas-scale) * 18px);
    box-sizing: border-box;
  }

  .alert__copy {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: calc(var(--canvas-scale) * 5px);
    min-width: 0;
  }

  .alert__copy :global(.bb-chip) {
    padding: calc(var(--canvas-scale) * 3px) calc(var(--canvas-scale) * 7px);
    font-size: calc(var(--canvas-scale) * 8px);
  }

  .alert__title {
    color: var(--bb-white);
    font-family: var(--bb-font-body);
    font-size: calc(var(--canvas-scale) * 15px);
    font-weight: 700;
    line-height: 1.2;
  }

  .alert__subtitle {
    color: var(--bb-muted);
    font-size: calc(var(--canvas-scale) * 11.5px);
  }

  .chat {
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: calc(var(--canvas-scale) * 5px);
    height: 100%;
    padding: calc(var(--canvas-scale) * 12px) calc(var(--canvas-scale) * 14px);
    box-sizing: border-box;
    overflow: hidden;
  }

  .chat__line {
    margin: 0;
    overflow: hidden;
    font-size: calc(var(--canvas-scale) * 7px);
    line-height: 1.4;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chat__user {
    margin-right: calc(var(--canvas-scale) * 5px);
    color: var(--bb-tan-light);
    font-weight: 700;
  }

  .chat__text {
    color: var(--bb-white);
  }
</style>
