<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';

  let {
    label,
    tabindex = 0,
    onclick
  }: { label: string; tabindex?: number; onclick: (event: MouseEvent & { currentTarget: HTMLButtonElement }) => void } = $props();
</script>

<button class="add" type="button" aria-label={label} title={label} {tabindex} {onclick}>
  <Icon name="plus" size={14} strokeWidth={2.2} />
</button>

<style>
  .add {
    position: relative;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: var(--bb-radius-pill);
    background: var(--bb-green-glow);
    box-shadow: 0 2px 8px rgba(var(--bb-shadow-rgb), 0.6);
    color: var(--bb-black);
    cursor: pointer;
    transition: transform var(--bb-dur-fast) var(--bb-ease-out-expo);
  }
  .add::before {
    content: '';
    position: absolute;
    inset: -8px;
    border-radius: var(--bb-radius-pill);
  }
  .add:hover {
    transform: scale(1.12);
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
