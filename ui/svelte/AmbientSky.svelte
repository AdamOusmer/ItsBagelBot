<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { SvelteHTMLElements } from 'svelte/elements';
  // The onboarding atmosphere, shared by pages that need the same depth.
  // Props describe stage/pointer motion; copy and foreground artwork stay in the app.
  import LightField from './LightField.svelte';
  import '../styles/orbs.css';
  import '../styles/elements/ambient-sky.css';

  type Own = {
    /** Lateral stage offset, -1..1. */
    shift?: number;
    /** Pointer position relative to the viewport centre, -1..1. */
    px?: number;
    py?: number;
    /** Horizon warmth, clamped to 0..1. */
    progress?: number;
    leaving?: boolean;
    /** Contained fills a positioned ancestor; fixed fills the viewport. */
    position?: 'contained' | 'fixed';
    stillStars?: boolean;
    warmth?: number;
    class?: string;
  };

  let {
    shift = 0, px = 0, py = 0, progress = 0, leaving = false,
    position = 'fixed', stillStars = false, warmth = 0.7,
    class: className = '', ...rest
  }: Own & Omit<SvelteHTMLElements['div'], keyof Own> = $props();

  const classes = $derived(['bb-ambient-sky', `bb-ambient-sky--${position}`, leaving && 'bb-ambient-sky--leaving', stillStars && 'bb-ambient-sky--still-stars', className].filter(Boolean).join(' '));
  const variables = $derived(`--ambient-shift: ${shift}; --ambient-px: ${px}; --ambient-py: ${py}; --ambient-progress: ${Math.min(Math.max(progress, 0), 1)};`);
</script>

<div
  class={classes}
  aria-hidden="true"
  style={variables} {...rest}
>
  <div class="bb-ambient-sky__dawn"></div>
  <div class="bb-ambient-sky__field"><LightField {warmth} /></div>
  <div class="bb-orb bb-orb--halo bb-orb--green bb-ambient-sky__orb bb-ambient-sky__orb--green"></div>
  <div class="bb-orb bb-orb--halo bb-orb--tan bb-ambient-sky__orb bb-ambient-sky__orb--tan"></div>
</div>
