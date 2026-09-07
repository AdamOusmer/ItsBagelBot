<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Icon from './Icon.svelte';
  import type { IconName } from '../lib/icons';
  let { icon, onclick, class: cls = '', ...rest }:
    { icon: IconName; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<button class="mini {cls}" {onclick} {...rest}><Icon name={icon} size={15} /></button>

<style>
  .mini { width: 28px; height: 28px; border-radius: var(--bb-radius-sm); display: flex; align-items: center; justify-content: center; background: transparent; border: 1px solid transparent; color: var(--bb-muted); cursor: pointer; transition: all var(--bb-dur-base) var(--bb-ease-out-expo); }
  .mini :global(svg) { width: 15px; height: 15px; stroke: currentColor; fill: none; stroke-width: 1.7; }
  .mini:hover { background: transparent; border-color: rgba(201,168,124,0.28); color: var(--bb-tan-light); }
</style>
