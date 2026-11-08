<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';

  let {
    label,
    tabindex = 0,
    showLabel = false,
    onclick
  }: { label: string; tabindex?: number; showLabel?: boolean; onclick: (event: MouseEvent & { currentTarget: HTMLButtonElement }) => void } = $props();
</script>

<button class="add" class:labelled={showLabel} type="button" aria-label={label} title={label} {tabindex} {onclick}>
  <Icon name="plus" size={14} strokeWidth={2} />
  {#if showLabel}<span>{label}</span>{/if}
</button>

<style>
  .add {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    box-sizing: border-box;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid rgba(var(--bb-white-rgb), 0.25);
    border-radius: var(--bb-radius-pill);
    background: var(--bb-card-bg);
    box-shadow: 0 2px 8px rgba(var(--bb-shadow-rgb), 0.35);
    color: var(--bb-white);
    cursor: pointer;
    transition: border-color var(--bb-dur-fast) var(--bb-ease-out-expo),
      color var(--bb-dur-fast) var(--bb-ease-out-expo),
      background-color var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .add.labelled {
    width: max-content;
    padding: 0 11px 0 9px;
    font-family: var(--bb-font-body);
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
  }
  .add::before {
    content: '';
    position: absolute;
    inset: -4px;
    border-radius: var(--bb-radius-pill);
  }
  :global(.canvas[data-touch]) .add.labelled::before {
    inset: -10px;
  }
  .add:hover {
    border-color: var(--bb-green-glow);
    color: var(--bb-green-glow);
  }
  .add:focus-visible {
    outline: 2px solid var(--bb-green-glow);
    outline-offset: 3px;
  }
  @media (prefers-reduced-motion: reduce) {
    .add {
      transition: none;
    }
  }
</style>
