<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import '../styles/elements/reading-progress.css';
  import { mountReadingProgress } from '../lib/reading-progress';

  let { class: className = '', ...rest }: { class?: string; [key: string]: unknown } = $props();

  let fill = $state<HTMLElement>();

  $effect(() => {
    if (!fill) return;
    return mountReadingProgress(fill);
  });

  const classes = $derived(['bb-reading-progress', className || null].filter(Boolean).join(' '));
</script>

<div class={classes} aria-hidden="true" {...rest}><span
    class="bb-reading-progress__fill"
    data-reading-progress
    bind:this={fill}
  ></span></div>
