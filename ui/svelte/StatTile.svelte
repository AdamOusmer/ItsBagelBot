<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  import { countUp } from '../lib/count-up';

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
