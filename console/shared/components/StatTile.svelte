<script lang="ts">
  import Icon from './Icon.svelte';
  import { countUp } from '../lib/actions';
  import type { IconName } from '../lib/icons';
  let {
    icon,
    tan = false,
    label,
    value,
    unit = '',
    delta,
    flat = false
  }: {
    icon: IconName;
    tan?: boolean;
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
    <span class="ico {tan ? 'tan' : ''}"><Icon name={icon} size={14} /></span>
  </div>
  <div class="value"><span use:countUp>{value}</span>{#if unit}<small>{unit}</small>{/if}</div>
  <div class="delta {flat ? 'flat' : ''}">{delta}</div>
</div>

<style>
  .strip-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 14px; }
  .ico { display: inline-flex; color: var(--bb-green-glow); opacity: 0.8; }
  .ico :global(svg) { width: 14px; height: 14px; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; stroke: currentColor; }
  .ico.tan { color: var(--bb-tan-light); }
</style>
