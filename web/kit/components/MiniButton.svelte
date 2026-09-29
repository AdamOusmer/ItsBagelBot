<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.
  import IconButton from '@bagel/ui/svelte/IconButton.svelte';
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import type { IconName } from '@bagel/ui/lib/icons';
  let { icon, 'aria-label': label, ...rest }:
    { icon: IconName; 'aria-label': string; onclick?: () => void; class?: string; [key: string]: unknown } = $props();
</script>

<IconButton size="sm" {label} {...rest}><Icon name={icon} size={15} /></IconButton>
