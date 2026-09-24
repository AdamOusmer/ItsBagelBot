<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/typography.css';
  import type { Snippet } from 'svelte';

  let {
    size = 'md',
    tone = 'default',
    mono = false,
    as: tag = 'p',
    class: className = '',
    children,
    ...rest
  }: {
    size?: 'xs' | 'sm' | 'md' | 'lg' | 'xl';
    tone?: 'default' | 'muted' | 'accent' | 'danger';
    mono?: boolean;
    as?: 'p' | 'span' | 'small' | 'div' | 'li' | 'dd' | 'dt';
    class?: string;
    children: Snippet;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-text',
      `bb-text--${size}`,
      tone === 'default' ? null : `bb-text--${tone}`,
      mono ? 'bb-text--mono' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {...rest}>{@render children()}</svelte:element>
