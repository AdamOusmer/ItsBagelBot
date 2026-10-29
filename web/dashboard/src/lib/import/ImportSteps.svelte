<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Text from '@bagel/ui/svelte/Text.svelte';

  let { items }: { items: readonly string[] } = $props();
</script>

{#if items.length}
  <ol class="steps">
    {#each items as s, i (i)}<Text as="li" size="sm">{s}</Text>{/each}
  </ol>
{/if}

<style>
  .steps {
    margin: 6px 0 16px;
    padding-left: 22px;
    display: flex;
    flex-direction: column;
    gap: 9px;
    overflow-wrap: anywhere;
  }
</style>
