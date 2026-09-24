<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Button from '../../svelte/Button.svelte';
  import ButtonLink from '../../svelte/ButtonLink.svelte';
  let { label = 'Save', icon = false, link = false, ...props }:
    { label?: string; icon?: boolean; link?: boolean; [key: string]: unknown } = $props();
</script>

{#if link}<ButtonLink {...props} href="/x">{label}</ButtonLink>{:else if icon}<Button {...props} aria-label={label}><svg viewBox="0 0 24 24"></svg></Button>{:else}<Button {...props}>{label}</Button>{/if}
