<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  type Own = {
    level?: 1 | 2 | 3 | 4 | 5 | 6;
    variant?: 'display' | 'section' | 'card' | 'title' | 'eyebrow' | 'label';
    uppercase?: boolean;
    as?: 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6' | 'p' | 'div' | 'span';
    element?: HTMLElement | null;
    class?: string;
    children: Snippet;
  };

  let {
    level = 2,
    variant,
    uppercase = false,
    as: tag,
    element = $bindable(null),
    class: className = '',
    children,
    ...rest
  }: Own & Omit<SvelteHTMLElements['h2'], keyof Own> = $props();

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
