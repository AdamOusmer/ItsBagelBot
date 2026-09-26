<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Composition only: callers supply live data, copy and their own artwork.
  // The shared layout owns responsive placement and the onboarding sky.
  import type { Snippet } from 'svelte';
  import AmbientSky from './AmbientSky.svelte';
  import '../styles/elements/stats-page-layout.css';

  let {
    heading, crowd, counters, ranking, community, notice, footer,
    arrangement = 'playful', class: className = '', ...rest
  }: {
    heading: Snippet;
    /** Up to six decorative figures, each inside a direct child wrapper. */
    crowd?: Snippet;
    /** The pair of lifetime counters. */
    counters: Snippet;
    ranking: Snippet;
    community: Snippet;
    notice?: Snippet;
    /** An optional footnote, rather than the site's navigation footer. */
    footer?: Snippet;
    arrangement?: 'playful' | 'onboarding' | 'gathering';
    class?: string;
    [key: string]: unknown;
  } = $props();

  const classes = $derived(['bb-stats-page', `bb-stats-page--${arrangement}`, className].filter(Boolean).join(' '));
</script>

<div class={classes} {...rest}>
  <AmbientSky position="fixed" progress={0.65} turn={24} />
  <div class="bb-stats-page__inner">
    <header class="bb-stats-page__header">
      <div class="bb-stats-page__heading">{@render heading()}</div>
      {#if crowd}<div class="bb-stats-page__crowd" aria-hidden="true">{@render crowd()}</div>{/if}
    </header>
    {#if notice}<div class="bb-stats-page__notice">{@render notice()}</div>{/if}
    <div class="bb-stats-page__body">
      <div class="bb-stats-page__counters">{@render counters()}</div>
      <div class="bb-stats-page__ranking">{@render ranking()}</div>
      <div class="bb-stats-page__community">{@render community()}</div>
    </div>
    {#if footer}<div class="bb-stats-page__footer">{@render footer()}</div>{/if}
  </div>
</div>
