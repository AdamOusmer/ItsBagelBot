<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { enhance, type SubmitFunction } from '$app/forms';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ManagementRow from '@bagel/ui/svelte/ManagementRow.svelte';
  import Switch from '@bagel/ui/svelte/Switch.svelte';
  import Tag from '@bagel/ui/svelte/Tag.svelte';
  import Text from '@bagel/ui/svelte/Text.svelte';
  import { getI18n, type MessageKey } from '@bagel/kit';
  import type { Automation } from '@bagel/kit/automations';
  import RowDeleteButton from '#lib/components/shared/RowDeleteButton.svelte';
  import { MANUAL_RUN_TRIGGERS, triggerSummary } from './model';

  const { t } = getI18n();

  let {
    automation,
    usable,
    toggleSubmit,
    onRun,
    onDelete
  }: {
    automation: Automation;
    usable: boolean;
    toggleSubmit: SubmitFunction;
    onRun: () => void;
    onDelete: () => void;
  } = $props();

  const trigger = $derived(automation.definition.trigger);
  const summary = $derived(triggerSummary(trigger));
  const runnable = $derived(MANUAL_RUN_TRIGGERS.includes(trigger.type));
  const actionCount = $derived(automation.definition.actions?.length ?? 0);
  const conditionCount = $derived(automation.definition.conditions?.length ?? 0);
</script>

<ManagementRow as="article" href={`/automations/${automation.id}`} label={`${t('automations.edit')}: ${automation.name}`}>
  {#snippet primary()}
    <span class="prow">
      <span class="name"><Text as="span" size="sm" truncate>{automation.name}</Text></span>
      <span class="trig">
        <Tag tone="bare">{t(`automations.trigger.${trigger.type}` as MessageKey)}</Tag>
        <span class="trig-val"><Text as="span" size="xs" mono tone="muted" truncate>{summary}</Text></span>
      </span>
      <span class="counts"><Text as="span" size="xs" tone="muted" truncate>{t('automations.rowCounts', { conditions: String(conditionCount), actions: String(actionCount) })}</Text></span>
    </span>
  {/snippet}
  {#snippet actions()}
    <span class="run-slot">
      {#if runnable}
        <Button size="sm" variant="secondary" disabled={!usable || !automation.enabled} onclick={onRun}>{t('automations.runNow')}</Button>
      {/if}
    </span>
    <form method="POST" action="?/setEnabled" use:enhance={toggleSubmit}>
      <input type="hidden" name="id" value={automation.id} />
      <input type="hidden" name="revision" value={automation.revision} />
      <input type="hidden" name="enabled" value={automation.enabled ? '' : 'on'} />
      <Switch type="submit" checked={automation.enabled} disabled={!usable} label={t('automations.toggleAria', { name: automation.name })} />
    </form>
    <RowDeleteButton label={t('automations.deleteAria', { name: automation.name })} disabled={!usable} onclick={onDelete} />
  {/snippet}
</ManagementRow>

<style>
  .prow { display: grid; grid-template-columns: minmax(0, 1fr) 260px 128px; align-items: center; column-gap: 14px; min-height: 40px; }
  .name, .trig-val, .counts { min-width: 0; display: block; overflow: hidden; }
  .trig { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; column-gap: 8px; min-width: 0; }
  .run-slot { display: inline-flex; width: 104px; justify-content: flex-end; }

  @media (max-width: 760px) {
    .prow { grid-template-columns: minmax(0, 1fr); row-gap: 4px; }
  }
</style>
