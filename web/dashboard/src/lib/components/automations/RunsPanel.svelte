<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, fmtDateTime, type MessageKey } from '@bagel/kit';
  import type { RunRecord } from '@bagel/kit/automations';
  import { OUTCOME_TONE, STATUS_TONE } from './model';

  const { t } = getI18n();

  let { runs, onRefresh, busy = false }: { runs: RunRecord[]; onRefresh: () => void; busy?: boolean } = $props();
</script>

<section class="runs" aria-label={t('automations.runsTitle')}>
  <div class="runs-head">
    <Text as="span" size="sm">{t('automations.runsTitle')}</Text>
    <Button size="sm" variant="ghost" {busy} onclick={onRefresh}>{t('automations.runsRefresh')}</Button>
  </div>
  <div class="runs-body">
    {#if runs.length === 0}
      <div class="runs-empty"><Text as="span" size="xs" tone="muted">{t('automations.runsEmpty')}</Text></div>
    {:else}
      <ul class="runs-list">
        {#each runs as run (run.run_id)}
          <li class="run">
            <span class="run-top">
              <Text as="span" size="xs" mono tone="muted">{fmtDateTime(run.at)}</Text>
              <Tag tone={OUTCOME_TONE[run.outcome] ?? 'quiet'}>{t(`automations.outcome.${run.outcome}` as MessageKey)}</Tag>
              <Text as="span" size="xs" mono tone="muted">{t('automations.runDuration', { ms: String(run.duration_ms) })}</Text>
              <span class="run-reason"><Text as="span" size="xs" tone="muted" truncate>{run.reason ?? ''}</Text></span>
            </span>
            <span class="run-actions">
              {#each run.actions ?? [] as a (a.index)}
                <span title={a.detail ?? ''}>
                  <Tag tone={STATUS_TONE[a.status] ?? 'quiet'}>{`${a.index + 1} ${t(`automations.action.${a.type}` as MessageKey)}: ${t(`automations.status.${a.status}` as MessageKey)}`}</Tag>
                </span>
              {/each}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<style>
  .runs { border: 1px solid var(--bb-glass-border); border-radius: var(--bb-radius-md); overflow: hidden; }
  .runs-head { display: flex; align-items: center; justify-content: space-between; min-height: 48px; padding: 0 12px; border-bottom: 1px solid var(--bb-glass-border); }
  .runs-body { height: 360px; overflow-y: auto; }
  .runs-empty { height: 100%; display: flex; align-items: center; justify-content: center; }
  .runs-list { list-style: none; margin: 0; padding: 0; }
  .run { display: flex; flex-direction: column; justify-content: center; gap: 4px; height: 64px; padding: 0 12px; border-bottom: 1px solid var(--bb-glass-border); overflow: hidden; }
  .run-top { display: grid; grid-template-columns: 180px 96px 72px minmax(0, 1fr); align-items: center; column-gap: 10px; }
  .run-reason { min-width: 0; display: block; overflow: hidden; }
  .run-actions { display: flex; flex-wrap: nowrap; gap: 6px; overflow: hidden; height: 22px; align-items: center; }
  .run-actions > * { flex: none; }
</style>
