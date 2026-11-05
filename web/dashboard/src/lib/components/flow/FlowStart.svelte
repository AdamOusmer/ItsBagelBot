<script lang="ts" module>
  export const START_W = 96;
  export const START_H = 36;
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import FlowAddButton from './FlowAddButton.svelte';
  import Icon from './FlowIcon.svelte';

  let { x, y, empty = false, onadd }: { x: number; y: number; empty?: boolean; onadd?: (byKey: boolean) => void } = $props();

  const { t } = getI18n();
</script>

<div class="start" class:empty data-flow-ui data-pan-through style:transform="translate({x}px, {y}px)" style:width="{START_W}px" style:height="{START_H}px">
  <Icon name="play" size={12} />
  <span>{t('flows.board.start')}</span>
  <span class="add">
    <FlowAddButton label={t('flows.board.addTrigger')} onclick={(e) => onadd?.(e.detail === 0)} />
  </span>
</div>

<style>
  .start {
    position: absolute;
    left: 0;
    top: 0;
    z-index: 1;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    border-radius: var(--bb-radius-pill);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.07), rgba(var(--bb-white-rgb), 0.03)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-2), 0 6px 18px rgba(var(--bb-shadow-rgb), 0.6);
    color: var(--bb-green-glow);
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    user-select: none;
  }
  .add {
    position: absolute;
    left: 100%;
    top: 50%;
    margin: -12px 0 0 -12px;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .start:hover::after {
    content: '';
    position: absolute;
    top: -8px;
    bottom: -8px;
    left: 100%;
    width: 24px;
  }
  .start.empty .add,
  .start:hover .add,
  .start:focus-within .add {
    opacity: 1;
    pointer-events: auto;
  }
  :global(.canvas[data-touch]) .start {
    z-index: 3;
  }
  :global(.canvas[data-touch]) .add {
    top: auto;
    bottom: 100%;
    left: 50%;
    margin: 0 0 6px -12px;
    opacity: 1;
    pointer-events: auto;
    transform-origin: center bottom;
    scale: calc(var(--inv, 1) * 1.25);
  }
  :global(.canvas[data-touch]) .start:hover::after {
    display: none;
  }
  @media (prefers-reduced-motion: reduce) {
    .add {
      transition: none;
    }
  }
</style>
