<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/tooltip.css';
  import type { Snippet } from 'svelte';

  let {
    text,
    placement = 'top',
    id,
    class: className = '',
    children,
    ...rest
  }: {
    text: string;
    placement?: 'top' | 'bottom';
    id?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    ['bb-tooltip', placement === 'bottom' ? 'bb-tooltip--bottom' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} {...rest}>{@render children()}<span
    class="bb-tooltip__bubble"
    {id}
    role={id ? 'tooltip' : undefined}
    aria-hidden={id ? undefined : 'true'}>{text}</span
  ></span>
