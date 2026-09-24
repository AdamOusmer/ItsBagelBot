<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/management-row.css';
  import type { Snippet } from 'svelte';

  let {
    selected = false,
    expanded = false,
    controls,
    disabled = false,
    accent = false,
    class: className = '',
    onselect,
    primary,
    actions,
    ...rest
  }: {
    selected?: boolean;
    expanded?: boolean;
    controls?: string;
    disabled?: boolean;
    accent?: boolean;
    class?: string;
    onselect?: () => void;
    primary?: Snippet;
    actions?: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-row',
      'row-shell',
      accent ? 'bb-row--accent' : null,
      selected ? 'is-selected' : null,
      disabled ? 'is-off' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<div class={classes} {...rest}><button
    class="bb-row__primary"
    type="button"
    data-cursor="quiet"
    aria-expanded={expanded}
    aria-controls={controls}
    aria-current={selected ? 'true' : undefined}
    onclick={onselect}
  >{#if primary}{@render primary()}{/if}</button>{#if actions}<div class="bb-row__actions"
    >{@render actions()}</div
  >{/if}</div>
