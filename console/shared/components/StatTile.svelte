<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import { countUp } from '../lib/actions';
  let {
    label,
    value,
    unit = '',
    delta,
    flat = false
  }: {
    label: string;
    value: string;
    unit?: string;
    delta: string;
    flat?: boolean;
  } = $props();
</script>

<!-- Channel-strip readout: the oversized numeral IS the tile. Rendered as a
     bare .stat cell so the parent .stat-grid draws the shared rules between
     strips (see app.css). -->
<div class="stat">
  <div class="strip-head">
    <span class="label">{label}</span>
  </div>
  <div class="value"><span use:countUp>{value}</span>{#if unit}<small>{unit}</small>{/if}</div>
  <div class="delta {flat ? 'flat' : ''}">{delta}</div>
</div>

<style>
  .strip-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 14px; }
</style>
