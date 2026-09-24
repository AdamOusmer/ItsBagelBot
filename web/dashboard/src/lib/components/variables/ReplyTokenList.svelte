<script lang="ts">
	// Copyright (c) 2026 Adam Ousmer. All rights reserved.
	// Proprietary. No license granted. See LICENSE.md.

  import { getI18n } from '@bagel/kit';
  import type { VariableChip } from '@bagel/kit/variables';

  let { chips }: { chips: readonly VariableChip[] } = $props();

  const { t } = getI18n();
</script>

{#if chips.length > 0}
  <ul class="reply-tokens">
    {#each chips as c (c.token)}
      <li class="row">
        <span class="bb-chip bb-chip--muted row-token">{c.token}</span>
        {#if c.hintKey}<span class="row-hint">{t(c.hintKey)}</span>{/if}
      </li>
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
    gap: 4px;
  }
  .row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .row-token { pointer-events: none; }
  .row-hint {
    flex: 1;
    min-width: 80px;
    font-family: var(--bb-font-body);
    font-size: 11.5px;
    color: var(--bb-muted);
  }
</style>
