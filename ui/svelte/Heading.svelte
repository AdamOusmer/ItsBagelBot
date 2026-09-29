<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    level = 2,
    variant,
    uppercase = false,
    as: tag,
    element = $bindable(null),
    class: className = '',
    children,
    ...rest
  }: {
    level?: 1 | 2 | 3 | 4 | 5 | 6;
    variant?: 'display' | 'section' | 'card' | 'title' | 'eyebrow' | 'label';
    uppercase?: boolean;
    as?: string;
    element?: HTMLElement | null;
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const tagName = $derived(tag ?? `h${level}`);
  const classes = $derived(
    [
      'bb-h',
      `bb-h--l${level}`,
      variant ? `bb-h--${variant}` : null,
      uppercase ? 'bb-h--upper' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tagName} bind:this={element} class={classes} {...rest}>{@render children()}</svelte:element>
