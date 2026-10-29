<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import VarCard from '@bagel/ui/svelte/VarCard.svelte';
  import { getI18n } from '@bagel/kit';
  import { chipHint, type VariableChip } from '@bagel/kit/variables';

  let { chips }: { chips: readonly VariableChip[] } = $props();

  const { t } = getI18n();
</script>

{#if chips.length > 0}
  <ul class="reply-tokens">
    {#each chips as c (c.token)}
      <li><VarCard token={c.token} hint={c.hintKey ? chipHint(t, c) : undefined} /></li>
    {/each}
  </ul>
{/if}

<style>
  .reply-tokens {
    list-style: none;
    margin: 8px 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
</style>
