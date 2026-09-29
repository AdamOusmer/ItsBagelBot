<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/brackets.css';

  type Own = {
    variant?: 'page' | 'loader';
    label?: string;
    class?: string;
  };

  let {
    variant = 'page',
    label,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const text = $derived(label ?? '');
  const classes = $derived(
    ['bb-ornaments', `bb-ornaments--${variant}`, className || null].filter(Boolean).join(' '),
  );
</script>

<div class={classes} aria-hidden="true" {...rest}><div class="bb-corner bb-corner--bl"></div><div
    class="bb-corner bb-corner--br"
  ></div><div class="bb-ornament-label">{text}</div></div>
