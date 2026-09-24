<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/layout.css';

  let {
    size = 4,
    grow = false,
    class: className = '',
    ...rest
  }: {
    size?: 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8;
    grow?: boolean;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(
    [
      'bb-spacer',
      grow ? 'bb-spacer--grow' : `bb-spacer--${size}`,
      className || null,
    ]
      .filter(Boolean)
      .join(' '),
  );
</script>

<span class={classes} aria-hidden="true" {...rest}></span>
