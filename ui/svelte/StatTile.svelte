<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  //
  // Channel-strip readout: the oversized numeral IS the tile. Rendered as a
  // bare .bb-stat cell so the parent .bb-stat-grid draws the shared rules
  // between strips (see ui/styles/elements/stat-tile.css).
  import type { Snippet } from 'svelte';
  import { countUp } from '../lib/count-up';

  // data-count-up is redundant here -- use:countUp already binds the engine --
  // and it is emitted anyway because the Astro twin has nothing BUT the
  // attribute to mark the numeral with, and the parity test diffs rendered
  // HTML. One marker, two adapters.

  let {
    label,
    value,
    unit = '',
    delta,
    flat = false,
    trail = undefined as Snippet | undefined,
    ...rest
  }: {
    label: string;
    value: string;
    unit?: string;
    delta: string;
    flat?: boolean;
    /** Optional right-hand slot in the head row (a badge, a menu). */
    trail?: Snippet;
    [key: string]: unknown;
  } = $props();
</script>

<div class="bb-stat" {...rest}>
  <div class="bb-stat__head">
    <span class="bb-stat__label">{label}</span>
    {#if trail}{@render trail()}{/if}
  </div>
  <div class="bb-stat__value"><span data-count-up use:countUp>{value}</span>{#if unit}<small>{unit}</small>{/if}</div>
  <div class="bb-stat__delta" data-flat={flat ? '' : undefined}>{delta}</div>
</div>
