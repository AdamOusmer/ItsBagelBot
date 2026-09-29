<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/skip-link.css';

  let {
    href,
    label,
    class: className = '',
    ...rest
  }: {
    href: string;
    label: string;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-skip-link', className || null].filter(Boolean).join(' '));
</script>

<a class={classes} {href} {...rest}>{label}</a>
