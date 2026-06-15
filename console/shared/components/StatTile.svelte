<script lang="ts">
  import Icon from './Icon.svelte';
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

<div class="card stat">
  <div class="ico {tan ? 'tan' : ''}"><Icon name={icon} size={18} /></div>
  <div class="label">{label}</div>
  <div class="value">{value}{#if unit}<small>{unit}</small>{/if}</div>
  <div class="delta {flat ? 'flat' : ''}">{delta}</div>
</div>
