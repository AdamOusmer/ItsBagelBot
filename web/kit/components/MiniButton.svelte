<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  let { icon, onclick, class: cls = '', ...rest }:
    { icon: IconName; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<button class="bb-btn bb-btn--icon bb-btn--sm {cls}" data-mark="" {onclick} {...rest}><Icon name={icon} size={15} /></button>
