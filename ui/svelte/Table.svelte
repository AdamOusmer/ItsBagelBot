<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/table.css';
  import type { Snippet } from 'svelte';

  let {
    label,
    zebra = false,
    compact = false,
    roomy = false,
    minWidth,
    class: className = '',
    children,
    ...rest
  }: {
    label: string;
    zebra?: boolean;
    compact?: boolean;
    roomy?: boolean;
    minWidth?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-tbl',
      zebra ? 'bb-tbl--zebra' : null,
      compact ? 'bb-tbl--compact' : null,
      roomy ? 'bb-tbl--roomy' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div class="bb-tbl-wrap" role="region" aria-label={label} tabindex="0"><table
    class={classes}
    style={minWidth ? `--tbl-min-w: ${minWidth};` : undefined}
    {...rest}>{@render children()}</table
  ></div>
