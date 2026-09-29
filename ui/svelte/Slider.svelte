<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import '../styles/elements/slider.css';

  let {
    value = $bindable(0),
    min = 0,
    max = 100,
    step = 1,
    class: className = '',
    ...rest
  }: {
    value?: number;
    min?: number;
    max?: number;
    step?: number;
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-slider', className || null].filter(Boolean).join(' '));
</script>

<input type="range" class={classes} {min} {max} {step} bind:value {...rest} />
