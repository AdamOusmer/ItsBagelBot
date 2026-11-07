<script lang="ts" module>
  export const START_W = 96;
  export const START_H = 36;
</script>

<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { getI18n } from '@bagel/kit';
  import Icon from './FlowIcon.svelte';

  let { x, y, touch = false, el = $bindable<HTMLElement | null>(null), onadd }: {
    x: number;
    y: number;
    empty?: boolean;
    touch?: boolean;
    el?: HTMLElement | null;
    onadd?: (byKey: boolean) => void;
  } = $props();

  const { t } = getI18n();
</script>

<button class="start" class:touch bind:this={el} type="button" aria-label={t('flows.board.addTrigger')} title={t('flows.board.addTrigger')} style:transform="translate({x}px, {y}px)" style:width="{START_W}px" style:height="{START_H}px" onclick={(e) => onadd?.(e.detail === 0)}>
  <Icon name="play" size={12} />
  <span>{t('flows.board.start')}</span>
  <span class="plus"><Icon name="plus" size={12} /></span>
</button>

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
    padding: 0;
    border: 0;
    border-radius: var(--bb-radius-pill);
    background: linear-gradient(rgba(var(--bb-white-rgb), 0.07), rgba(var(--bb-white-rgb), 0.03)), var(--bb-card-bg);
    box-shadow: var(--bb-depth-2), 0 6px 18px rgba(var(--bb-shadow-rgb), 0.6);
    color: var(--bb-green-glow);
    font-family: var(--bb-font-body);
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
    user-select: none;
  }
  .plus {
    display: flex;
    color: rgba(var(--bb-white-rgb), 0.5);
    transition: color var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .start:hover .plus,
  .start:focus-visible .plus {
    color: var(--bb-green-glow);
  }
  .start:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: 3px;
  }
  .start.touch {
    z-index: 3;
  }
  .start.touch::after {
    content: '';
    position: absolute;
    top: min(0px, calc((36px - 44px * var(--inv, 1)) / 2));
    bottom: min(0px, calc((36px - 44px * var(--inv, 1)) / 2));
    left: min(0px, calc((96px - 44px * var(--inv, 1)) / 2));
    right: min(0px, calc((96px - 44px * var(--inv, 1)) / 2));
    border-radius: var(--bb-radius-pill);
  }
  @media (prefers-reduced-motion: reduce) {
    .plus {
      transition: none;
    }
  }
</style>
