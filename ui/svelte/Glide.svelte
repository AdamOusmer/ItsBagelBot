<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { HTMLAttributes } from 'svelte/elements';
  import '../styles/elements/glide.css';

  type Own = {
    shown?: boolean;
    class?: string;
  };

  let {
    shown = false,
    class: className = '',
    ...rest
  }: Own & Omit<HTMLAttributes<HTMLSpanElement>, keyof Own> = $props();

  const classes = $derived(['bb-glide', shown ? 'is-shown' : null, className || null].filter(Boolean).join(' '));
</script>

<span class={classes} aria-hidden="true" {...rest}></span>
