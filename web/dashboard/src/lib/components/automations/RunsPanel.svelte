<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import Button from '@bagel/ui/svelte/Button.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, fmtDateTime, type MessageKey } from '@bagel/kit';
  import type { RunRecord } from '@bagel/kit/automations';
  import type { Flow } from '#lib/flow/model';
  import { explainRun, explainStep, runStepRows } from '#lib/flow/runs';
  import { OUTCOME_TONE, STATUS_TONE } from './model';

  const { t } = getI18n();

  let {
    runs,
    onRefresh,
    busy = false,
    flow,
    showNames = false,
    title,
    empty
  }: {
    runs: RunRecord[];
    onRefresh: () => void;
    busy?: boolean;
    flow?: Flow;
    showNames?: boolean;
    title?: string;
    empty?: string;
  } = $props();
</script>

<section class="runs" aria-label={title ?? t('automations.runsTitle')}>
  <div class="runs-head">
    <Text as="span" size="sm">{title ?? t('automations.runsTitle')}</Text>
    <Button size="sm" variant="ghost" {busy} onclick={onRefresh}>{t('automations.runsRefresh')}</Button>
  </div>
  <div class="runs-body">
    {#if runs.length === 0}
      <div class="runs-empty"><Text as="span" size="xs" tone="muted">{empty ?? t('automations.runsEmpty')}</Text></div>
    {:else}
      <ul class="runs-list">
        {#each runs as run (run.run_id)}
          <li class="run">
            <span class="run-top">
              <Tag tone={OUTCOME_TONE[run.outcome] ?? 'quiet'}>{t(`automations.outcome.${run.outcome}` as MessageKey)}</Tag>
              {#if showNames}<Text as="span" size="xs">{run.name}</Text>{/if}
              <Text as="span" size="xs" mono tone="muted">{fmtDateTime(run.at)}</Text>
              <Text as="span" size="xs" mono tone="muted">{t('automations.runDuration', { ms: String(run.duration_ms) })}</Text>
            </span>
            {#if run.reason}<Text as="p" size="xs" tone="muted">{explainRun(t, run.reason)}</Text>{/if}
            <ul class="run-steps">
              {#each runStepRows(t, run.actions ?? [], flow) as step (step.key)}
                {@const why = explainStep(t, step.status, step.detail)}
                <li class="run-step">
                  <span class="step-top">
                    <Tag tone={STATUS_TONE[step.status] ?? 'quiet'}>{t(`automations.status.${step.status}` as MessageKey)}</Tag>
                    <Text as="span" size="xs">{step.name ?? t(`automations.action.${step.type}` as MessageKey)}</Text>
                    {#if step.count > 1}<Text as="span" size="xs" mono tone="muted">{t('flows.recipe.runCount', { count: step.count })}</Text>{/if}
                  </span>
                  {#if step.line}<span class="step-line"><Text as="span" size="xs" tone="muted" truncate>{step.line}</Text></span>{/if}
                  {#if why}<Text as="p" size="xs" tone="muted">{why}</Text>{/if}
                </li>
              {/each}
            </ul>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</section>

<style>
  .runs { border: 1px solid var(--bb-glass-border); border-radius: var(--bb-radius-md); overflow: hidden; }
  .runs-head { display: flex; align-items: center; justify-content: space-between; min-height: 48px; padding: 0 12px; border-bottom: 1px solid var(--bb-glass-border); }
  .runs-body { height: 360px; overflow-y: auto; overscroll-behavior: contain; }
  .runs-empty { height: 100%; display: flex; align-items: center; justify-content: center; padding: 0 12px; text-align: center; }
  .runs-list { list-style: none; margin: 0; padding: 0; }
  .run { display: flex; flex-direction: column; gap: 8px; padding: 12px; border-bottom: 1px solid var(--bb-glass-border); }
  .run-top { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 10px; }
  .run-steps { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; }
  .run-step { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .step-top { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; }
  .step-line { display: block; min-width: 0; overflow: hidden; }
</style>
