<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/spinner.css';

  let {
    size = 'sm',
    class: className = '',
    ...rest
  }: {
    size?: 'sm' | 'md';
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-spinner', size === 'md' ? 'bb-spinner--md' : null, className || null].filter(Boolean).join(' '));
</script>

<span class={classes} aria-hidden="true" {...rest}></span>
