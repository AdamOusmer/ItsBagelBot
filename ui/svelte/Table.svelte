<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/table.css';
  import type { Snippet } from 'svelte';

  let {
    label,
    zebra = false,
    compact = false,
    class: className = '',
    children,
    ...rest
  }: {
    label: string;
    zebra?: boolean;
    compact?: boolean;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-tbl',
      zebra ? 'bb-tbl--zebra' : null,
      compact ? 'bb-tbl--compact' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="bb-tbl-wrap" role="region" aria-label={label} tabindex="0"><table
    class={classes}
    {...rest}>{@render children()}</table
  ></div>
