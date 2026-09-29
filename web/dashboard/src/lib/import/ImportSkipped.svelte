<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { Tag, Text } from '@bagel/kit';
  import type { CollisionRef } from '@bagel/kit';
  import { capSkipped } from './helpers';
  import type { Translate } from './session.svelte';

  let { skipped, t }: { skipped: CollisionRef[] | undefined; t: Translate } = $props();
  const view = $derived(capSkipped(skipped));
  const total = $derived(skipped?.length ?? 0);
</script>

{#if total > 0}
  <div class="skipped">
    <Text size="sm" tone="muted">{t('import.skippedHead', { n: total })}</Text>
    <ul class="skipped-list">
      {#each view.shown as c (c.kind + c.name)}
        <li class="skipped-row">
          <Text as="span" size="sm" mono>{c.name}</Text>
          <Tag tone="error">{t('import.alreadyExists')}</Tag>
        </li>
      {/each}
    </ul>
    {#if view.more > 0}<Text size="xs" mono tone="muted">{t('import.skippedMore', { n: view.more })}</Text>{/if}
  </div>
{/if}

<style>
  .skipped { display: grid; gap: 8px; text-align: left; }
  .skipped-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .skipped-row { display: flex; align-items: center; gap: 10px; min-height: 28px; overflow-wrap: anywhere; }
</style>
