<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { Tag } from '@bagel/kit';
  import type { CollisionRef } from '@bagel/kit';
  import { capSkipped } from './helpers';
  import type { Translate } from './session.svelte';

  let { skipped, t }: { skipped: CollisionRef[] | undefined; t: Translate } = $props();
  const view = $derived(capSkipped(skipped));
  const total = $derived(skipped?.length ?? 0);
</script>

{#if total > 0}
  <div class="skipped">
    <p class="skipped-head">{t('import.skippedHead', { n: total })}</p>
    <ul class="skipped-list">
      {#each view.shown as c (c.kind + c.name)}
        <li>
          <span class="skipped-name">{c.name}</span>
          <Tag tone="error">{t('import.alreadyExists')}</Tag>
        </li>
      {/each}
    </ul>
    {#if view.more > 0}<p class="skipped-more">{t('import.skippedMore', { n: view.more })}</p>{/if}
  </div>
{/if}

<style>
  .skipped { display: grid; gap: 8px; text-align: left; }
  .skipped-head { margin: 0; color: var(--bb-muted); }
  .skipped-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .skipped-list li { display: flex; align-items: center; gap: 10px; min-height: 28px; }
  .skipped-name { font-family: var(--bb-font-mono); overflow-wrap: anywhere; }
  .skipped-more { margin: 0; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 12px; }
</style>
