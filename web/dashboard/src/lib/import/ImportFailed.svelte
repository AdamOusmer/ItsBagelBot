<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { Button, Tag } from '@bagel/kit';
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
    <p class="failed-head">{t('import.failedHead', { n: total })}</p>
    <ul class="failed-list">
      {#each view.shown as f (f.kind + f.name)}
        <li>
          <span class="failed-name">{f.name}</span>
          <Tag tone="error">{t(REASON_KEY[f.reason])}</Tag>
        </li>
      {/each}
    </ul>
    {#if view.more > 0}<p class="failed-more">{t('import.skippedMore', { n: view.more })}</p>{/if}
    {#if retryCount > 0}
      <div class="failed-actions">
        <Button variant="secondary" onclick={onRetry} disabled={submitting} loading={submitting}>
          {t('import.retryFailed', { n: retryCount })}
        </Button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .failed { display: grid; gap: 8px; text-align: left; }
  .failed-head { margin: 0; color: var(--bb-muted); }
  .failed-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 6px; }
  .failed-list li { display: flex; align-items: center; gap: 10px; min-height: 28px; }
  .failed-name { font-family: var(--bb-font-mono); overflow-wrap: anywhere; }
  .failed-more { margin: 0; color: var(--bb-muted); font-family: var(--bb-font-mono); font-size: 12px; }
  .failed-actions { display: flex; }
</style>
