<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    level = 2,
    variant,
    as: tag,
    class: className = '',
    children,
    ...rest
  }: {
    level?: 1 | 2 | 3 | 4 | 5 | 6;
    variant?: 'display' | 'section' | 'card' | 'eyebrow';
    as?: string;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const element = $derived(tag ?? `h${level}`);
  const classes = $derived(
    ['bb-h', `bb-h--l${level}`, variant ? `bb-h--${variant}` : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={element} class={classes} {...rest}>{@render children()}</svelte:element>
