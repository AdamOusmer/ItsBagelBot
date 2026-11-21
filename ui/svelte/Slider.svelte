<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  import '../styles/elements/slider.css';

  type Own = {
    value?: number;
    min?: number;
    max?: number;
    step?: number;
    class?: string;
  };

  let {
    value = $bindable(0),
    min = 0,
    max = 100,
    step = 1,
    class: className = '',
    ...rest
  }: Own & Omit<SvelteHTMLElements['input'], keyof Own> = $props();

  const classes = $derived(['bb-slider', className || null].filter(Boolean).join(' '));
  const progress = $derived(max > min ? Math.min(1, Math.max(0, (Number(value) - min) / (max - min))) : 0);
</script>

<input type="range" class={classes} {min} {max} {step} bind:value style:--slider-progress={progress} {...rest} />
