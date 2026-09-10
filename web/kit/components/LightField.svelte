<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { onMount } from 'svelte';
  // The physics live in the design library and are shared verbatim with the
  // marketing site's script/lightfield.js: console surfaces have to read as
  // the same material as itsbagelbot.com, and the two hand-kept copies this
  // replaces had already drifted on how a mote is counted warm. What stays
  // here is only the Svelte lifecycle.
  import { field } from '@bagel/ui/lib/light-field';

  // `warmth` is the share of gold (vs green) motes, matching the web field's
  // data-warmth. 0.7 is the pricing-header value.
  let { warmth = 0.7 }: { warmth?: number } = $props();

  let canvas: HTMLCanvasElement;

  // field() returns null under reduced motion (and when there is no 2D
  // context); onMount is happy with an undefined teardown, and the CSS below
  // hides the canvas in that case anyway.
  onMount(() => field(canvas, { warmth }) ?? undefined);
</script>

<canvas bind:this={canvas} class="light-field" aria-hidden="true"></canvas>

<style>
  .light-field {
    position: absolute;
    z-index: -1;
    inset: 0;
    display: block;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }

  @media (prefers-reduced-motion: reduce) {
    .light-field { display: none; }
  }
</style>
