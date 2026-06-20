<script lang="ts">
  import Icon from './Icon.svelte';
  import Card from './Card.svelte';
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

<Card stat>
  <div class="ico {tan ? 'tan' : ''}"><Icon name={icon} size={18} /></div>
  <div class="label">{label}</div>
  <div class="value">{value}{#if unit}<small>{unit}</small>{/if}</div>
  <div class="delta {flat ? 'flat' : ''}">{delta}</div>
</Card>

<style>
  .ico { width: 36px; height: 36px; border-radius: 9px; background: rgba(82,183,136,0.10); border: 1px solid rgba(82,183,136,0.28);
    display: flex; align-items: center; justify-content: center; margin-bottom: 18px; }
  .ico.tan { background: rgba(201,168,124,0.10); border-color: rgba(201,168,124,0.3); }
  .ico :global(svg) { width: 18px; height: 18px; fill: none; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; stroke: var(--bb-green-glow); }
  .ico.tan :global(svg) { stroke: var(--bb-tan-light); }
  .label { font-family: var(--bb-font-mono); font-size: 10.5px; letter-spacing: 0.16em; text-transform: uppercase; color: var(--bb-muted); }
  .value { font-family: var(--bb-font-display); font-weight: 700; font-size: 34px; letter-spacing: -0.02em; color: var(--bb-white); margin-top: 8px; line-height: 1; }
  .value small { font-size: 16px; font-weight: 500; color: var(--bb-muted); margin-left: 4px; letter-spacing: 0; }
  .delta { font-family: var(--bb-font-mono); font-size: 11px; color: var(--bb-green-glow); margin-top: 10px; letter-spacing: 0.02em; }
  .delta.flat { color: var(--bb-muted); }
</style>
