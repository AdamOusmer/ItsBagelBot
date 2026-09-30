<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import type { ImportFailedItem } from '@bagel/kit';
  import { capFailed } from './helpers';
  import type { Translate } from './session.svelte';

  let {
    failed,
    retryCount,
    submitting,
    onRetry,
    t
  }: {
    failed: ImportFailedItem[] | undefined;
    retryCount: number;
    submitting: boolean;
    onRetry: () => void;
    t: Translate;
  } = $props();
  const REASON_KEY = {
    rejected: 'import.failedReason.rejected',
    invalid: 'import.failedReason.invalid',
    module: 'import.failedReason.module'
  } as const;
  const view = $derived(capFailed(failed));
  const total = $derived(failed?.length ?? 0);
</script>

{#if total > 0}
  <div class="failed">
    <Text size="sm" tone="muted">{t('import.failedHead', { n: total })}</Text>
    <ul class="failed-list">
      {#each view.shown as f (f.kind + f.name)}
        <li class="failed-row">
          <Text as="span" size="sm" mono>{f.name}</Text>
          <Tag tone="danger">{t(REASON_KEY[f.reason])}</Tag>
        </li>
      {/each}
    </ul>
    {#if view.more > 0}<Text size="xs" mono tone="muted">{t('import.skippedMore', { n: view.more })}</Text>{/if}
    {#if retryCount > 0}
      <div class="failed-actions">
        <Button variant="secondary" onclick={onRetry} disabled={submitting} busy={submitting}>
          {t('import.retryFailed', { n: retryCount })}
        </Button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .failed { display: grid; gap: 8px; text-align: left; }
  .failed-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .failed-row { display: flex; align-items: center; gap: 10px; min-height: 28px; overflow-wrap: anywhere; }
  .failed-actions { display: flex; }
</style>
