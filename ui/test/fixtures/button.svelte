<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  // Svelte half of the Button parity fixture; its Astro twin is ./button.astro.
  // A fixture rather than rendering the adapter directly because children are a
  // snippet on one side and a slot on the other, and hand-building a snippet in
  // the test would be testing the test.
  import Button from '../../svelte/Button.svelte';
  import ButtonLink from '../../svelte/ButtonLink.svelte';
  let { label = 'Save', icon = false, link = false, ...props }:
    { label?: string; icon?: boolean; link?: boolean; [key: string]: unknown } = $props();
</script>

{#if link}<ButtonLink {...props} href="/x">{label}</ButtonLink>{:else if icon}<Button {...props} aria-label={label}><svg viewBox="0 0 24 24"></svg></Button>{:else}<Button {...props}>{label}</Button>{/if}
