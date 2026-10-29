<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/halo-panel.css';

  type Own = {
    as?: 'div' | 'a' | 'article' | 'section' | 'aside';
    href?: string;
    tone?: 'gold' | 'green';
    class?: string;
    children?: Snippet;
  };

  let {
    as,
    href,
    tone = 'gold',
    class: className = '',
    children,
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLElement>, keyof Own> = $props();

  const tag = $derived(as ?? (href ? 'a' : 'div'));
  const classes = $derived(
    ['bb-halo-panel', tone === 'green' ? 'bb-halo-panel--green' : null, className || null]
      .filter(Boolean)
      .join(' '),
  );
</script>

<svelte:element this={tag} class={classes} {href} {...rest}><span class="bb-halo-panel__halo" aria-hidden="true"></span>{@render children?.()}</svelte:element>
