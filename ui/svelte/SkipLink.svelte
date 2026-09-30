<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/skip-link.css';

  type Own = {
    href: string;
    label: string;
    class?: string;
  };

  let {
    href,
    label,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['a'], keyof Own> = $props();

  const classes = $derived(['bb-skip-link', className || null].filter(Boolean).join(' '));
</script>

<a class={classes} {href} {...rest}>{label}</a>
