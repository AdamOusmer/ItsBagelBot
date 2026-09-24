<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';

  let {
    vertical = false,
    flush = false,
    fade = false,
    class: className = '',
    ...rest
  }: {
    vertical?: boolean;
    flush?: boolean;
    fade?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-divider',
      vertical ? 'bb-divider--v' : null,
      flush ? 'bb-divider--flush' : null,
      fade ? 'bb-divider--fade' : null,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

{#if vertical}<span class={classes} aria-hidden="true" {...rest}></span>{:else}<hr
    class={classes}
    {...rest}
  />{/if}
