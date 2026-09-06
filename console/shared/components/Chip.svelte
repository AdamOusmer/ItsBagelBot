<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import type { Snippet } from 'svelte';
  let { on = false, onclick, class: cls = '', children, ...rest }:
    { on?: boolean; onclick?: () => void; class?: string; children: Snippet; [key: string]: unknown } = $props();
</script>

<button class="bb-chip {on ? 'is-on' : ''} {cls}" {onclick} {...rest}>{@render children()}</button>

<style>
  /* Was a pill (--bb-radius-pill) with a translucent fill; the frame is now the
     global Tier-2 control (.bb-chip). `on` keeps its meaning, drawn as the
     tan-forward active state rather than a filled pill. */
  .is-on { color: var(--bb-tan-pale); background: rgba(201,168,124,0.10); border-color: rgba(201,168,124,0.50); }
</style>
