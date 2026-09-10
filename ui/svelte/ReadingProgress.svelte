<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte adapter for the `.bb-reading-progress` contract
  // (../styles/elements/reading-progress.css). Astro twin:
  // ../astro/ReadingProgress.astro.
  //
  // Where the Astro adapter hoists one script that scans for the contract
  // attribute, this one binds the element and drives it from an $effect: the
  // component instance IS the lifetime, so there is nothing to scan for and
  // nothing to re-bind after a navigation.
  //
  // `data-reading-progress` is emitted anyway, and not as decoration — it is
  // the contract attribute the Astro adapter's scan keys on, and holding both
  // adapters to the same markup is what ../test/parity.test.ts checks.
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
