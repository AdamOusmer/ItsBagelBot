<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.

  import Button from '../../svelte/Button.svelte';
  import ButtonLink from '../../svelte/ButtonLink.svelte';
  let { label = 'Save', link = false, ...props }:
    { label?: string; link?: boolean; [key: string]: unknown } = $props();
</script>

{#if link}<ButtonLink {...props} href="/x">{label}</ButtonLink>{:else}<Button {...props}>{label}</Button>{/if}
