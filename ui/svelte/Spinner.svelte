<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/spinner.css';

  type Own = {
    size?: 'sm' | 'md';
    class?: string;
  };

  let {
    size = 'sm',
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['span'], keyof Own> = $props();

  const classes = $derived(['bb-spinner', size === 'md' ? 'bb-spinner--md' : null, className || null].filter(Boolean).join(' '));
</script>

<span class={classes} aria-hidden="true" {...rest}></span>
