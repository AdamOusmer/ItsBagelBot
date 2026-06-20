<script lang="ts">
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let { icon, onclick, class: cls = '', ...rest }:
    { icon: IconName; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<button class="mini {cls}" {onclick} {...rest}><Icon name={icon} size={15} /></button>

<style>
  .mini { width: 28px; height: 28px; border-radius: 7px; display: flex; align-items: center; justify-content: center; background: none; border: 1px solid transparent; color: var(--bb-muted); cursor: pointer; transition: all var(--bb-dur-base) var(--bb-ease-out-expo); }
  .mini :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .mini:hover { color: var(--bb-white); background: rgba(255,255,255,0.05); }
</style>
