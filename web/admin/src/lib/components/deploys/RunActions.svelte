<script lang="ts">
  // Copyright (c) 2026 Adam Ousmer. All rights reserved.
  // Proprietary. No license granted. See LICENSE.md.
  import { applyAction, enhance } from '$app/forms';
  import type { SubmitFunction } from '@sveltejs/kit';
  import AlertBanner from '@bagel/ui/svelte/AlertBanner.svelte';
  import Button from '@bagel/ui/svelte/Button.svelte';
  import ConfirmDialog from '@bagel/ui/svelte/ConfirmDialog.svelte';
  import { toast } from '@bagel/ui/svelte/toast';
  import { actionPayload, adminToastFailure } from '@bagel/kit';
  import { getI18n } from '@bagel/kit/i18n/context';
  import type { DeployRun } from '$lib/deploys/types';
  import { approvalStage, cancellable, failedStage, isTerminal, offered } from './view';

  let { run, onrun }: { run: DeployRun; onrun: (run: DeployRun) => void } = $props();

  const { t } = getI18n();
  const failed = adminToastFailure(toast);

  type Verb = {
    id: string;
    action: '?/resume' | '?/cancel' | '?/approve';
    label: string;
    fields: Record<string, string>;
    variant: 'primary' | 'secondary' | 'ghost';
  };

  const acts = $derived(offered(run));
  const failure = $derived(run.failure ?? failedStage(run)?.failure);
  const approval = $derived(approvalStage(run));
  const stoppedAt = $derived(failedStage(run)?.id ?? '');
  const rollbackTo = $derived(run.outputs.live_version ?? '');
  const canCancel = $derived(cancellable(run, acts));
  const canRollback = $derived(acts.has('rollback') && rollbackTo !== '');

  const verbs = $derived.by(() => {
    const all: (Verb | false)[] = [
      approval !== undefined && {
        id: 'approve',
        action: '?/approve',
        label: t('admin.deploys.act.approve'),
        fields: { run_id: run.id, stage: approval.id },
        variant: 'primary'
      },
      acts.has('resume') && {
        id: 'resume',
        action: '?/resume',
        label: t('admin.deploys.act.resume'),
        fields: { run_id: run.id },
        variant: 'primary'
      },
      acts.has('rerun_failed_jobs') && {
        id: 'retry',
        action: '?/resume',
        label: t('admin.deploys.act.retry'),
        fields: { run_id: run.id, stage: stoppedAt, rerun: 'true' },
        variant: 'secondary'
      },
      canCancel && {
        id: 'cancel',
        action: '?/cancel',
        label: t('admin.deploys.act.cancel'),
        fields: { run_id: run.id },
        variant: 'ghost'
      }
    ];
    return all.filter((v): v is Verb => v !== false);
  });

  let busy = $state(false);
  let rollbackOpen = $state(false);
  let rollbackForm = $state<HTMLFormElement | null>(null);

  const submitVerb: SubmitFunction = () => {
    busy = true;
    return async ({ result }) => {
      busy = false;
      const p = actionPayload<{ run?: DeployRun; error?: string }>(result);
      if (p?.run) return onrun(p.run);
      failed(p, t('admin.deploys.actionFailed'));
    };
  };

  const submitRollback: SubmitFunction = () => {
    busy = true;
    rollbackOpen = false;
    return async ({ result }) => {
      busy = false;
      if (result.type !== 'failure') return applyAction(result);
      failed(actionPayload(result), t('admin.deploys.startFailed'));
    };
  };
</script>

<div class="slot">
  {#if failure}
    <AlertBanner>{failure.message}</AlertBanner>
  {:else if run.cancel_requested && !isTerminal(run.state)}
    <AlertBanner variant="warn" role="status">{t('admin.deploys.cancelRequested')}</AlertBanner>
  {/if}
</div>

<div class="bar">
  {#each verbs as v (v.id)}
    <form method="POST" action={v.action} use:enhance={submitVerb}>
      {#each Object.entries(v.fields) as [name, value] (name)}
        <input type="hidden" {name} {value} />
      {/each}
      <Button type="submit" variant={v.variant} size="sm" disabled={busy}>{v.label}</Button>
    </form>
  {/each}
  {#if canRollback}
    <form method="POST" action="/deploys?/start" use:enhance={submitRollback} bind:this={rollbackForm}>
      <input type="hidden" name="kind" value="rollback" />
      <input type="hidden" name="rollback_to" value={rollbackTo} />
      <Button variant="destructive" size="sm" disabled={busy} onclick={() => (rollbackOpen = true)}>
        {t('admin.deploys.act.rollback', { version: rollbackTo })}
      </Button>
    </form>
  {/if}
</div>

<ConfirmDialog
  open={rollbackOpen}
  danger
  title={t('admin.deploys.rollbackTitle')}
  body={t('admin.deploys.rollbackBody', { version: rollbackTo })}
  confirmLabel={t('admin.deploys.act.rollback', { version: rollbackTo })}
  cancelLabel={t('admin.deploys.confirmCancel')}
  busyLabel={t('admin.deploys.shipBusy')}
  {busy}
  onConfirm={() => rollbackForm?.requestSubmit()}
  onCancel={() => (rollbackOpen = false)}
/>

<style>
  .slot {
    margin-bottom: 8px;
  }
  .slot:empty {
    display: none;
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-start;
    gap: 8px;
    min-height: 36px;
  }
  .bar form {
    display: contents;
  }
</style>
