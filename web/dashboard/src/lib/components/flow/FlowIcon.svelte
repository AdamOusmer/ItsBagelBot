<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Icon from '@bagel/ui/svelte/Icon.svelte';
  import { flowIcons, type FlowIconName } from '#lib/flow/icons';

  let { name, size = 16 }: { name: FlowIconName; size?: number } = $props();
</script>

{#if name in flowIcons}
  <!-- {@html} is safe only because the bodies are constants, never user input. -->
  <svg
    class="bb-icon"
    viewBox="0 0 24 24"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.6"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true">{@html flowIcons[name as keyof typeof flowIcons]}</svg
  >
{:else}
  <Icon name={name as Exclude<FlowIconName, keyof typeof flowIcons>} {size} />
{/if}
